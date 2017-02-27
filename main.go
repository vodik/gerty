package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/naoina/toml"
	"github.com/quadrifoglio/go-qmp"
	"github.com/vishvananda/netlink"
)

type Netdev struct {
	Model  string
	Subnet string
}

type Drive struct {
	File   string
	Model  string `toml:"model,omitempty"`
	Media  string `toml:"media,omitempty"`
	Format string `toml:"format,omitempty"`
	Cache  string `toml:"cache,omitempty"`
}

type Spice struct {
	Port int
}

type tomlConfig struct {
	SMP     int `toml:"smp"`
	Memory  string
	Spice   Spice
	Netdevs []Netdev `toml:"netdev,omitempty"`
	Drives  []Drive  `toml:"drive,omitempty"`
}

func (c Netdev) BuildArgs() []string {
	return []string{
		"-netdev", fmt.Sprintf("tap,id=%s,ifname=%s,script=no,downscript=no",
			"tap0", "tap0"),
		"-device", fmt.Sprintf("virtio-net,netdev=%s", "tap0"),
	}
}

func (c Drive) BuildArgs() []string {
	if c.Media == "cdrom" {
		return []string{
			"-drive", fmt.Sprintf("file=%s,media=%s",
				c.File, c.Media),
		}
	}
	return []string{
		"-drive", fmt.Sprintf("format=%s,file=%s,cache=%s,if=%s",
			c.Format, c.File, c.Cache, c.Model),
	}
}

func (c Spice) BuildArgs() []string {
	return []string{
		"-vga", "qxl",
		"-spice", fmt.Sprintf("port=%d,disable-ticketing", c.Port),
	}
}

type QemuConfig interface {
	BuildArgs() []string
}

func StartQemu(config tomlConfig, snapshot bool) (cmd *exec.Cmd, err error) {
	fullArgs := []string{
		"--enable-kvm",
		"-cpu", "host", "-smp", strconv.Itoa(config.SMP),
		"-m", config.Memory,
		"-boot", "order=d",
		"-monitor", "none",
		"-qmp", "unix:/run/qmp,server,nowait"}

	fullArgs = append(fullArgs, config.Spice.BuildArgs()...)

	for _, c := range config.Netdevs {
		fullArgs = append(fullArgs, c.BuildArgs()...)
	}

	for _, c := range config.Drives {
		fullArgs = append(fullArgs, c.BuildArgs()...)
	}

	if snapshot {
		fullArgs = append(fullArgs, "-snapshot")
	}

	cmd = exec.Command("/usr/bin/qemu-system-x86_64", fullArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err = cmd.Start()
	if err != nil {
		return
	}

	return cmd, nil
}

func WaitQemu() (res string, ret int, err error) {
	var (
		status syscall.WaitStatus
		usage  syscall.Rusage
	)

	_, err = syscall.Wait4(-1, &status, syscall.WNOHANG, &usage)
	if err != nil {
		return
	}

	res, ret = "", -1
	switch {
	case status.Exited():
		ret = status.ExitStatus()
		res = "exit status " + strconv.Itoa(ret)
	case status.Signaled():
		res = "signal: " + status.Signal().String()
	}

	if status.CoreDump() {
		res += " (core dumped)"
	}

	return res, ret, nil
}

func PowerDown() (err error) {
	sock, err := qmp.Open("unix", "/run/qmp")
	if err != nil {
		return
	}

	defer sock.Close()

	_, err = sock.Command("system_powerdown", nil)
	if err != nil {
		return
	}

	return nil
}

func main() {
	snapshotFlag := flag.Bool("snapshot", false, "snapshot disks")
	configFile := flag.String("c", "", "config file to load")
	flag.Parse()

	f, err := os.Open(*configFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	buf, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}
	var config tomlConfig
	if err := toml.Unmarshal(buf, &config); err != nil {
		panic(err)
	}

	signal_chan := make(chan os.Signal, 1)
	signal.Notify(signal_chan,
		syscall.SIGCHLD,
		syscall.SIGINT,
		syscall.SIGTERM)

	tap, err := CreateNetwork(config.Netdevs[0].Subnet)
	if err != nil {
		panic(err)
	}

	_, err = StartQemu(config, *snapshotFlag)
	if err != nil {
		panic(err)
	}

	exit_chan := make(chan int)
	shutting_down := false

	go func() {
		for {
			s := <-signal_chan
			switch s {
			case syscall.SIGCHLD:
				res, ret, err := WaitQemu()
				if err != nil {
					panic(err)
				}

				fmt.Printf("%s\n", res)
				exit_chan <- ret
			case syscall.SIGINT:
				if !shutting_down {
					fmt.Printf("Sending ACPI halt signal to vm...\n")
					if err = PowerDown(); err != nil {
						panic(err)
					}
					fmt.Printf("VM signalled to shutdown... Press Ctrl+C to shutdown immediately.\n")
					shutting_down = true
				} else {
					fmt.Println("Stopping...")
					exit_chan <- 0
				}
			case syscall.SIGTERM:
				fmt.Printf("Sending ACPI halt signal to vm...\n")
				if err = PowerDown(); err != nil {
					panic(err)
				}
				fmt.Printf("VM signalled to shutdown... Press Ctrl+C to shutdown immediately.\n")
				go func() {
					time.Sleep(time.Second * 60)
					fmt.Println("Timed out, stopping...")
					exit_chan <- 0
				}()
				shutting_down = true
			default:
				fmt.Println("Stopping...")
				exit_chan <- 0
			}
		}
	}()

	code := <-exit_chan
	netlink.LinkDel(tap)
	os.Exit(code)
}
