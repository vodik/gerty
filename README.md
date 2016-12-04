## gerty

A simple Go QEMU wrapper.

Define your VM with a simple toml config file (still WIP and kinda
crappy). Actually, the whole code base is a mess. My first Go program.

### Example config

```toml
memory = "1024"

[dhcp]
subnet = "172.18.47.1/30"  # TODO: move into ifaces

[spice]
port = 5900

[[disks]]
model = "virtio"
format = "qcow2"
image = "disk.qcow2"

[[ifaces]]
model = "virtio"
```
