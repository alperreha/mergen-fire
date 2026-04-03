package vsockcfg

// ShellPort is a guest-side AF_VSOCK service number.
// This is NOT a TCP/UDP network port and is not exposed outside the VM network stack.
const ShellPort uint32 = 7000

const ShellPortInt = int(ShellPort)

const (
	// ExecBeginMarker marks beginning of one-shot command frame sent by host.
	ExecBeginMarker = "__MERGEN_EXEC_BEGIN__"
	// ExecEndMarker marks end of one-shot command frame sent by host.
	ExecEndMarker = "__MERGEN_EXEC_END__"
	// ExecDonePrefix is appended with exit code by guest when one-shot command completes.
	ExecDonePrefix = "__MERGEN_EXEC_DONE__:"
	// DebugEnvVar enables verbose vsock logs when set to a truthy value.
	DebugEnvVar = "MERGEN_VSOCK_DEBUG"
)
