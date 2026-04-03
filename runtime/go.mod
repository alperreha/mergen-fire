module github.com/alperreha/mergen-fire/runtime

go 1.24.0

require (
	github.com/alperreha/mergen-fire v0.0.0
	github.com/vishvananda/netlink v1.3.1
	golang.org/x/sys v0.37.0
)

require github.com/vishvananda/netns v0.0.5 // indirect

replace github.com/alperreha/mergen-fire => ../
