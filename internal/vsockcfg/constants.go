package vsockcfg

// ShellPort is a guest-side AF_VSOCK service number.
// This is NOT a TCP/UDP network port and is not exposed outside the VM network stack.
const ShellPort uint32 = 7000

const ShellPortInt = int(ShellPort)
