package main

import (
	"flag"
	"fmt"
	"github.com/naoina/toml"
	"github.com/quadrifoglio/go-qmp"
	"github.com/vishvananda/netlink"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

type Iface struct {
	Model string
}

type Disk struct {
	Image  string
	Format string
	Model  string
}

type Spice struct {
	Port int
}

type tomlConfig struct {
	Memory string
	DHCP   networkConfig
	Spice  Spice
	Ifaces []Iface
	Disks  []Disk
}

func (c Iface) BuildArgs() []string {
	return []string{
		"-net", fmt.Sprintf("nic,model=%s", c.Model),
		"-net", fmt.Sprintf("tap,ifname=%s,script=no,downscript=no,vhost=on",
			"tap0"),
	}
}

func (c Disk) BuildArgs() []string {
	return []string{
		"-drive", fmt.Sprintf("format=%s,file=%s,cache=writeback,if=%s",
			c.Format, c.Image, c.Model),
	}
}

func (c Spice) BuildArgs() []string {
	return []string{
		"-vga", "qxl",
		"-spice", fmt.Sprintf("port=%d,disable-ticketing",
			c.Port),
	}
}

type QemuConfig interface {
	BuildArgs() []string
}

func StartQemu(config tomlConfig) (cmd *exec.Cmd, err error) {
	fullArgs := []string{
		"--enable-kvm", "-m", config.Memory,
		"-boot", "order=d",
		"-monitor", "none",
		"-qmp", "unix:/run/qmp,server,nowait"}

	fullArgs = append(fullArgs, config.Spice.BuildArgs()...)

	for _, c := range config.Ifaces {
		fullArgs = append(fullArgs, c.BuildArgs()...)
	}

	for _, c := range config.Disks {
		fullArgs = append(fullArgs, c.BuildArgs()...)
	}

	fmt.Println(fullArgs)
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

	tap, err := CreateNetwork(config.DHCP)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Starting QEMU!\n")
	_, err = StartQemu(config)
	if err != nil {
		panic(err)
	}

	exit_chan := make(chan int)
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
				fmt.Printf("Sending ACPI halt signal to vm...\n")
				if err = PowerDown(); err != nil {
					panic(err)
				}
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
