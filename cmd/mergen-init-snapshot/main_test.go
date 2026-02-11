package main

import "testing"

func TestMetadataPathFromCmdline(t *testing.T) {
	cmdline := "console=ttyS0 root=/dev/vdb mergen.meta=/etc/mergen/image-meta.json panic=1"
	got := metadataPathFromCmdline(cmdline)
	if got != "/etc/mergen/image-meta.json" {
		t.Fatalf("metadataPathFromCmdline() = %q, want %q", got, "/etc/mergen/image-meta.json")
	}
}

func TestParseEnvList(t *testing.T) {
	env := parseEnvList([]string{"A=1", "B=", "INVALID", " =x", "C=hello=world"})
	if env["A"] != "1" {
		t.Fatalf("env[A] = %q, want 1", env["A"])
	}
	if env["B"] != "" {
		t.Fatalf("env[B] = %q, want empty", env["B"])
	}
	if _, ok := env["INVALID"]; ok {
		t.Fatalf("INVALID key should not be parsed")
	}
	if env["C"] != "hello=world" {
		t.Fatalf("env[C] = %q, want hello=world", env["C"])
	}
}

func TestBuildSpecFromMetaFallback(t *testing.T) {
	meta := imageMeta{
		Entrypoint: []string{"python"},
		Cmd:        []string{"app.py"},
		Env:        []string{"FOO=bar"},
	}
	spec := buildSpecFromMeta(meta)
	if len(spec.Argv) != 2 || spec.Argv[0] != "python" || spec.Argv[1] != "app.py" {
		t.Fatalf("unexpected argv: %#v", spec.Argv)
	}
	if spec.User != "root" {
		t.Fatalf("spec.User = %q, want root", spec.User)
	}
	if spec.Env["FOO"] != "bar" {
		t.Fatalf("spec.Env[FOO] = %q, want bar", spec.Env["FOO"])
	}
}

func TestBuildSpecFromMetaStartCmdPriority(t *testing.T) {
	meta := imageMeta{
		Entrypoint: []string{"ignored"},
		Cmd:        []string{"ignored"},
		StartCmd:   []string{"/usr/bin/myapp", "--flag"},
	}
	spec := buildSpecFromMeta(meta)
	if len(spec.Argv) != 2 || spec.Argv[0] != "/usr/bin/myapp" || spec.Argv[1] != "--flag" {
		t.Fatalf("unexpected argv: %#v", spec.Argv)
	}
}
