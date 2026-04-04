package images

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/alperreha/mergen-fire/internal/config"
)

type S3Store struct {
	client        *s3.Client
	bucket        string
	prefix        string
	username      string
	progressEvery time.Duration
}

func NewS3Store(ctx context.Context, cfg config.Config) (*S3Store, error) {
	if strings.TrimSpace(cfg.S3Bucket) == "" {
		return nil, errors.New("MGR_S3_BUCKET is required")
	}
	if strings.TrimSpace(cfg.S3Username) == "" {
		return nil, errors.New("MGR_S3_USERNAME is required")
	}

	loadOpts := []func(*awscfg.LoadOptions) error{
		awscfg.WithRegion(strings.TrimSpace(cfg.S3Region)),
	}
	if strings.TrimSpace(cfg.S3AccessKeyID) != "" || strings.TrimSpace(cfg.S3SecretKey) != "" {
		loadOpts = append(loadOpts, awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			strings.TrimSpace(cfg.S3AccessKeyID),
			strings.TrimSpace(cfg.S3SecretKey),
			strings.TrimSpace(cfg.S3SessionToken),
		)))
	}

	awsConfig, err := awscfg.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		if endpoint := strings.TrimSpace(cfg.S3Endpoint); endpoint != "" {
			o.BaseEndpoint = awsv2.String(endpoint)
		}
		o.UsePathStyle = cfg.S3UsePathStyle
		o.HTTPClient = &http.Client{
			Timeout: 0,
		}
	})

	return &S3Store{
		client:        client,
		bucket:        strings.TrimSpace(cfg.S3Bucket),
		prefix:        strings.Trim(strings.TrimSpace(cfg.S3Prefix), "/"),
		username:      strings.TrimSpace(cfg.S3Username),
		progressEvery: cfg.ProgressEvery,
	}, nil
}

func (s *S3Store) BaseManifestKey() string {
	return s.joinKey(s.prefix, s.username, "base", "current", manifestFile)
}

func (s *S3Store) BaseFileKey(name string) string {
	return s.joinKey(s.prefix, s.username, "base", "current", filepath.ToSlash(name))
}

func (s *S3Store) PayloadManifestKey(imageRef string) string {
	return s.joinKey(s.prefix, s.username, "payload", escapeRef(imageRef), manifestFile)
}

func (s *S3Store) PayloadFileKey(imageRef, name string) string {
	return s.joinKey(s.prefix, s.username, "payload", escapeRef(imageRef), filepath.ToSlash(name))
}

func (s *S3Store) UploadFile(ctx context.Context, localPath, remoteKey string, reporter ProgressReporter, update ProgressUpdate) (ManifestFile, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("open %s: %w", localPath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return ManifestFile{}, fmt.Errorf("stat %s: %w", localPath, err)
	}

	update.RemoteKey = remoteKey
	update.LocalPath = localPath
	update.TotalBytes = info.Size()
	update.Status = "running"
	update.StartedAt = time.Now().UTC()
	update.UpdatedAt = update.StartedAt
	report(reporter, update)

	hasher := sha256.New()
	reader := newProgressReader(file, info.Size(), s.progressEvery, reporter, update, hasher)

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        awsv2.String(s.bucket),
		Key:           awsv2.String(remoteKey),
		Body:          reader,
		ContentLength: awsv2.Int64(info.Size()),
	})
	if err != nil {
		update.Status = "failed"
		update.Message = err.Error()
		update.UpdatedAt = time.Now().UTC()
		update.TransferredBytes = reader.Transferred()
		update.Percent = percent(update.TransferredBytes, update.TotalBytes)
		report(reporter, update)
		return ManifestFile{}, fmt.Errorf("put object %s: %w", remoteKey, err)
	}

	update.Status = "completed"
	update.UpdatedAt = time.Now().UTC()
	update.TransferredBytes = info.Size()
	update.Percent = 100
	report(reporter, update)

	return ManifestFile{
		Name:      update.FileName,
		Key:       remoteKey,
		SizeBytes: info.Size(),
		SHA256:    hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func (s *S3Store) DownloadFile(ctx context.Context, remoteKey, localPath string, reporter ProgressReporter, update ProgressUpdate) (ManifestFile, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: awsv2.String(s.bucket),
		Key:    awsv2.String(remoteKey),
	})
	if err != nil {
		return ManifestFile{}, fmt.Errorf("get object %s: %w", remoteKey, err)
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return ManifestFile{}, fmt.Errorf("mkdir %s: %w", filepath.Dir(localPath), err)
	}

	tempPath := localPath + ".partial"
	file, err := os.Create(tempPath)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("create %s: %w", tempPath, err)
	}

	update.RemoteKey = remoteKey
	update.LocalPath = localPath
	update.TotalBytes = awsv2.ToInt64(resp.ContentLength)
	update.Status = "running"
	update.StartedAt = time.Now().UTC()
	update.UpdatedAt = update.StartedAt
	report(reporter, update)

	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	transferred, copyErr := copyWithProgress(writer, resp.Body, awsv2.ToInt64(resp.ContentLength), s.progressEvery, reporter, update)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return ManifestFile{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return ManifestFile{}, fmt.Errorf("close %s: %w", tempPath, closeErr)
	}
	if err := os.Rename(tempPath, localPath); err != nil {
		_ = os.Remove(tempPath)
		return ManifestFile{}, fmt.Errorf("rename %s -> %s: %w", tempPath, localPath, err)
	}

	update.Status = "completed"
	update.UpdatedAt = time.Now().UTC()
	update.TransferredBytes = transferred
	update.Percent = percent(update.TransferredBytes, update.TotalBytes)
	report(reporter, update)

	return ManifestFile{
		Name:      update.FileName,
		Key:       remoteKey,
		SizeBytes: transferred,
		SHA256:    hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func (s *S3Store) PutManifest(ctx context.Context, key string, manifest Manifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	body = append(body, '\n')
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        awsv2.String(s.bucket),
		Key:           awsv2.String(key),
		Body:          strings.NewReader(string(body)),
		ContentLength: awsv2.Int64(int64(len(body))),
		ContentType:   awsv2.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("put manifest %s: %w", key, err)
	}
	return nil
}

func (s *S3Store) GetManifest(ctx context.Context, key string) (Manifest, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: awsv2.String(s.bucket),
		Key:    awsv2.String(key),
	})
	if err != nil {
		return Manifest{}, fmt.Errorf("get manifest %s: %w", key, err)
	}
	defer resp.Body.Close()

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", key, err)
	}
	return manifest, nil
}

func (s *S3Store) joinKey(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return path.Join(filtered...)
}

func (s *S3Store) NewManifest(kind, name string) Manifest {
	return Manifest{
		Kind:       kind,
		Name:       name,
		Username:   s.username,
		UploadedAt: time.Now().UTC(),
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func escapeRef(ref string) string {
	return url.PathEscape(strings.TrimSpace(ref))
}

func report(reporter ProgressReporter, update ProgressUpdate) {
	if reporter == nil {
		return
	}
	update.Percent = percent(update.TransferredBytes, update.TotalBytes)
	reporter.Report(update)
}

func percent(transferred, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(transferred) * 100 / float64(total)
}

type progressReader struct {
	reader     io.Reader
	hasher     io.Writer
	total      int64
	reporter   ProgressReporter
	base       ProgressUpdate
	every      time.Duration
	lastReport time.Time
	sent       int64
}

func newProgressReader(reader io.Reader, total int64, every time.Duration, reporter ProgressReporter, base ProgressUpdate, hasher io.Writer) *progressReader {
	if every <= 0 {
		every = 250 * time.Millisecond
	}
	return &progressReader{
		reader:     reader,
		hasher:     hasher,
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
		r.sent += int64(n)
		if r.hasher != nil {
			_, _ = r.hasher.Write(p[:n])
		}
		now := time.Now().UTC()
		if now.Sub(r.lastReport) >= r.every {
			update := r.base
			update.Status = "running"
			update.UpdatedAt = now
			update.TransferredBytes = r.sent
			report(r.reporter, update)
			r.lastReport = now
		}
	}
	return n, err
}

func (r *progressReader) Transferred() int64 {
	return r.sent
}

func copyWithProgress(dst io.Writer, src io.Reader, total int64, every time.Duration, reporter ProgressReporter, base ProgressUpdate) (int64, error) {
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
				update := base
				update.Status = "failed"
				update.Message = err.Error()
				update.TransferredBytes = transferred
				update.UpdatedAt = time.Now().UTC()
				report(reporter, update)
				return transferred, fmt.Errorf("write local file: %w", err)
			}
			transferred += int64(n)
			now := time.Now().UTC()
			if now.Sub(lastReport) >= every {
				update := base
				update.Status = "running"
				update.TransferredBytes = transferred
				update.TotalBytes = total
				update.UpdatedAt = now
				report(reporter, update)
				lastReport = now
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return transferred, nil
		}
		update := base
		update.Status = "failed"
		update.Message = readErr.Error()
		update.TransferredBytes = transferred
		update.UpdatedAt = time.Now().UTC()
		report(reporter, update)
		return transferred, fmt.Errorf("read remote file: %w", readErr)
	}
}
