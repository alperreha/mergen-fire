package converter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const payloadRootFSFileName = "payload-rootfs.ext4"

type TransferProgress struct {
	Registry         string
	Image            string
	FileName         string
	Direction        string
	Status           string
	TransferredBytes int64
	TotalBytes       int64
	Percent          float64
	Message          string
	UpdatedAt        time.Time
}

type ProgressReporter interface {
	Report(TransferProgress)
}

type ProgressReporterFunc func(TransferProgress)

func (f ProgressReporterFunc) Report(progress TransferProgress) {
	if f != nil {
		f(progress)
	}
}

type CLIProgressReporter struct {
	w  io.Writer
	mu sync.Mutex
}

func NewCLIProgressReporter(w io.Writer) *CLIProgressReporter {
	return &CLIProgressReporter{w: w}
}

func (r *CLIProgressReporter) Report(progress TransferProgress) {
	if r == nil || r.w == nil {
		return
	}
	line := fmt.Sprintf(
		"%s %-9s %-22s %s",
		progress.Direction,
		progress.Status,
		progress.FileName,
		humanProgress(progress.TransferredBytes, progress.TotalBytes),
	)
	r.mu.Lock()
	defer r.mu.Unlock()
	if progress.Status == "running" {
		_, _ = fmt.Fprintf(r.w, "\r%s", line)
		return
	}
	_, _ = fmt.Fprintf(r.w, "\r%s\n", line)
}

type PayloadTransferResult struct {
	Registry  string
	Image     string
	LocalPath string
	Bucket    string
	ObjectKey string
	SizeBytes int64
}

func PushPayloadToUserS3(ctx context.Context, registryAlias string, profile RegistryProfile, imageRef, localPath string, reporter ProgressReporter) (PayloadTransferResult, error) {
	if err := ValidateRegistryForTransfer(profile); err != nil {
		return PayloadTransferResult{}, err
	}
	client, err := newUserS3Client(ctx, profile)
	if err != nil {
		return PayloadTransferResult{}, err
	}

	file, err := os.Open(localPath)
	if err != nil {
		return PayloadTransferResult{}, fmt.Errorf("open payload rootfs %s: %w", localPath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return PayloadTransferResult{}, fmt.Errorf("stat payload rootfs %s: %w", localPath, err)
	}

	objectKey := payloadObjectKey(profile, imageRef)
	progress := TransferProgress{
		Registry:   registryAlias,
		Image:      imageRef,
		FileName:   payloadRootFSFileName,
		Direction:  "push",
		Status:     "running",
		TotalBytes: info.Size(),
		UpdatedAt:  time.Now().UTC(),
	}
	reportTransfer(reporter, progress)

	reader := newProgressReader(file, info.Size(), 250*time.Millisecond, reporter, progress)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        awsv2.String(profile.Bucket),
		Key:           awsv2.String(objectKey),
		Body:          reader,
		ContentLength: awsv2.Int64(info.Size()),
	})
	if err != nil {
		progress.Status = "failed"
		progress.Message = err.Error()
		progress.TransferredBytes = reader.transferred
		progress.UpdatedAt = time.Now().UTC()
		reportTransfer(reporter, progress)
		return PayloadTransferResult{}, fmt.Errorf("put payload object %s: %w", objectKey, err)
	}

	progress.Status = "completed"
	progress.TransferredBytes = info.Size()
	progress.Percent = 100
	progress.UpdatedAt = time.Now().UTC()
	reportTransfer(reporter, progress)

	return PayloadTransferResult{
		Registry:  registryAlias,
		Image:     imageRef,
		LocalPath: localPath,
		Bucket:    profile.Bucket,
		ObjectKey: objectKey,
		SizeBytes: info.Size(),
	}, nil
}

func PullPayloadFromUserS3(ctx context.Context, registryAlias string, profile RegistryProfile, imageRef, localPath string, reporter ProgressReporter) (PayloadTransferResult, error) {
	if err := ValidateRegistryForTransfer(profile); err != nil {
		return PayloadTransferResult{}, err
	}
	client, err := newUserS3Client(ctx, profile)
	if err != nil {
		return PayloadTransferResult{}, err
	}

	objectKey := payloadObjectKey(profile, imageRef)
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: awsv2.String(profile.Bucket),
		Key:    awsv2.String(objectKey),
	})
	if err != nil {
		return PayloadTransferResult{}, fmt.Errorf("get payload object %s: %w", objectKey, err)
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(pathDir(localPath), 0o755); err != nil {
		return PayloadTransferResult{}, fmt.Errorf("mkdir payload output dir: %w", err)
	}

	tempPath := localPath + ".partial"
	file, err := os.Create(tempPath)
	if err != nil {
		return PayloadTransferResult{}, fmt.Errorf("create temp payload file: %w", err)
	}

	totalBytes := awsv2.ToInt64(resp.ContentLength)
	progress := TransferProgress{
		Registry:   registryAlias,
		Image:      imageRef,
		FileName:   payloadRootFSFileName,
		Direction:  "pull",
		Status:     "running",
		TotalBytes: totalBytes,
		UpdatedAt:  time.Now().UTC(),
	}
	reportTransfer(reporter, progress)

	transferred, copyErr := copyWithProgress(file, resp.Body, totalBytes, 250*time.Millisecond, reporter, progress)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return PayloadTransferResult{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return PayloadTransferResult{}, fmt.Errorf("close temp payload file: %w", closeErr)
	}
	if err := os.Rename(tempPath, localPath); err != nil {
		_ = os.Remove(tempPath)
		return PayloadTransferResult{}, fmt.Errorf("move payload file into place: %w", err)
	}

	progress.Status = "completed"
	progress.TransferredBytes = transferred
	progress.Percent = 100
	progress.UpdatedAt = time.Now().UTC()
	reportTransfer(reporter, progress)

	return PayloadTransferResult{
		Registry:  registryAlias,
		Image:     imageRef,
		LocalPath: localPath,
		Bucket:    profile.Bucket,
		ObjectKey: objectKey,
		SizeBytes: transferred,
	}, nil
}

func payloadObjectKey(profile RegistryProfile, imageRef string) string {
	escapedUsername := url.PathEscape(strings.TrimSpace(profile.Username))
	escapedImage := url.PathEscape(strings.TrimSpace(imageRef))
	return path.Join(profile.Prefix, escapedUsername, "payload", escapedImage, payloadRootFSFileName)
}

func newUserS3Client(ctx context.Context, profile RegistryProfile) (*s3.Client, error) {
	loadOpts := []func(*awscfg.LoadOptions) error{
		awscfg.WithRegion(profile.Region),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			profile.AccessKeyID,
			profile.SecretAccessKey,
			profile.SessionToken,
		)),
	}
	awsConfig, err := awscfg.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsConfig, func(opts *s3.Options) {
		if endpoint := strings.TrimSpace(profile.Endpoint); endpoint != "" {
			opts.BaseEndpoint = awsv2.String(endpoint)
		}
		opts.UsePathStyle = profile.UsePathStyle
		opts.HTTPClient = &http.Client{Timeout: 0}
	})
	return client, nil
}

func reportTransfer(reporter ProgressReporter, progress TransferProgress) {
	if reporter == nil {
		return
	}
	progress.Percent = percentTransferred(progress.TransferredBytes, progress.TotalBytes)
	reporter.Report(progress)
}

func percentTransferred(transferred, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(transferred) * 100 / float64(total)
}

type progressReader struct {
	reader      io.Reader
	total       int64
	reporter    ProgressReporter
	base        TransferProgress
	every       time.Duration
	lastReport  time.Time
	transferred int64
}

func newProgressReader(reader io.Reader, total int64, every time.Duration, reporter ProgressReporter, base TransferProgress) *progressReader {
	if every <= 0 {
		every = 250 * time.Millisecond
	}
	return &progressReader{
		reader:     reader,
		total:      total,
		reporter:   reporter,
		base:       base,
		every:      every,
		lastReport: time.Now().UTC(),
	}
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.transferred += int64(n)
		now := time.Now().UTC()
		if now.Sub(r.lastReport) >= r.every {
			progress := r.base
			progress.Status = "running"
			progress.TransferredBytes = r.transferred
			progress.TotalBytes = r.total
			progress.UpdatedAt = now
			reportTransfer(r.reporter, progress)
			r.lastReport = now
		}
	}
	return n, err
}

func copyWithProgress(dst io.Writer, src io.Reader, total int64, every time.Duration, reporter ProgressReporter, base TransferProgress) (int64, error) {
	if every <= 0 {
		every = 250 * time.Millisecond
	}
	buf := make([]byte, 1024*1024)
	var transferred int64
	lastReport := time.Now().UTC()

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				progress := base
				progress.Status = "failed"
				progress.Message = err.Error()
				progress.TransferredBytes = transferred
				progress.TotalBytes = total
				progress.UpdatedAt = time.Now().UTC()
				reportTransfer(reporter, progress)
				return transferred, fmt.Errorf("write payload file: %w", err)
			}
			transferred += int64(n)

			now := time.Now().UTC()
			if now.Sub(lastReport) >= every {
				progress := base
				progress.Status = "running"
				progress.TransferredBytes = transferred
				progress.TotalBytes = total
				progress.UpdatedAt = now
				reportTransfer(reporter, progress)
				lastReport = now
			}
		}

		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return transferred, nil
		}

		progress := base
		progress.Status = "failed"
		progress.Message = readErr.Error()
		progress.TransferredBytes = transferred
		progress.TotalBytes = total
		progress.UpdatedAt = time.Now().UTC()
		reportTransfer(reporter, progress)
		return transferred, fmt.Errorf("read payload object: %w", readErr)
	}
}

func humanProgress(transferred, total int64) string {
	if total <= 0 {
		return fmt.Sprintf("%s transferred", humanBytes(transferred))
	}
	return fmt.Sprintf("%s / %s (%.1f%%)", humanBytes(transferred), humanBytes(total), percentTransferred(transferred, total))
}

func humanBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	idx := -1
	for size >= 1024 && idx < len(units)-1 {
		size /= 1024
		idx++
	}
	if idx < 0 {
		return fmt.Sprintf("%d B", value)
	}
	return fmt.Sprintf("%.1f %s", size, units[idx])
}

func pathDir(filePath string) string {
	dir := strings.TrimSpace(filepath.Dir(filePath))
	if dir == "" || dir == "." {
		return "."
	}
	return dir
}
