package main

import (
	"adb/logger"
	"fmt"
	"github.com/go-ping/ping"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var prot uint16 = 5037
var adb string

func main() {
	p, _ := os.Executable()
	adb = filepath.Dir(p) + "/adb/adb.exe"
	logger.Info(fmt.Sprintln(os.Args, " 程序路径：", p))
	if len(os.Args) > 1 {
		command := strings.Join(os.Args[1:], " ")
		if strings.Contains(command, "start-server") {
			if os.Getenv("ADB_PROXY_START_SERVER") != "" {
				if strings.Contains(command, "-P") {
					i, e := strconv.Atoi(os.Args[2])
					if e == nil {
						prot = uint16(i)
					}
				}
				start()
				return
			}
			restart()

			cmd := exec.Command(os.Args[0], os.Args[1:]...)
			cmd.Env = append(os.Environ(), "ADB_PROXY_START_SERVER=1")
			err := cmd.Start()
			if err != nil {
				os.Exit(1)
			}
			time.Sleep(200 * time.Millisecond)
			fmt.Printf("* daemon not running; starting now at tcp:%s\n* daemon started successfully\n", strconv.Itoa(int(prot)))
			//io.ReadAtLeast(r, make([]byte, 1024), 1)
			//defer r.Close()
			os.Exit(0)
		}
	}

	cmd := exec.Command(adb, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
	os.Exit(cmd.ProcessState.ExitCode())
}

func start() {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(int(prot)))
	if err != nil {
		return
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	// 正常运行
	//fmt.Printf("* daemon not running; starting now at tcp:%s\n* daemon started successfully\n", strconv.Itoa(int(prot)))
	var index int
	for {
		index++
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go cp(conn, "[fd"+strconv.Itoa(index)+"]")
	}
}

func cp(conn net.Conn, fd string) {
	defer conn.Close()
	logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " AT 新连接进来\r\n")
	defer func() {
		logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " AT 连接关闭\r\n")
	}()
	src, err := net.Dial("tcp", ":"+strconv.Itoa(int(prot+1)))
	if err != nil {
		os.Exit(0)
	}
	defer src.Close()

	f1, w1 := io.Pipe()
	defer w1.Close()
	defer f1.Close()
	a := io.MultiWriter(src, w1)

	f2, w2 := io.Pipe()
	defer w2.Close()
	defer f2.Close()
	b := io.MultiWriter(conn, w2)
	go dd(f2, fd+" RX ")

	go func() {
		defer conn.Close()

		for {
			l := make([]byte, 1024)
			i, err := f1.Read(l)
			if err != nil || i < 4 {
				logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " TX 读取发送数据失败：" + err.Error() + "\r\n")
				return
			}
			ll, err := strconv.ParseInt(string(l[:4]), 16, 64)
			if err != nil {
				logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " TX 长度转换失败：" + err.Error() + " " + string(l) + "\r\n")
				return
			}
			if int(ll)+4 > i {
				logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " TX 内容不足：" + strconv.Itoa(int(ll)) + "\r\n")
				la := make([]byte, int(ll)+4-i)
				io.ReadFull(f1, la)
				if i < 1024 {
					ii := copy(l[i:], la)
					la = la[:ii]
					i += ii
				}
				if len(la) > 0 {
					l = append(l, la...)
					i += len(la)
				}
			}
			data := l[4:i]
			s := strings.Split(string(data), ":")
			if len(s) < 2 {
				return
			}
			logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " TX " + string(l[:i]) + "\r\n")
			var status string
			if s[1] != "kill" {
				// 接收
				i, _ = io.ReadFull(src, l[:4])
				b.Write(l[:i])
				status = string(l[:i])
			}
			//if err != nil || string(l[:i]) != "OKAY" {
			//	io.Copy(b, src)
			//	return
			//}

			switch s[0] {
			case "shell":
				if len(s) > 1 && s[1] == "getprop" {
					// 探测是否有异常
					seek := make([]byte, 1024*1024)
					n, e := src.Read(seek)
					logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " SE 探查的数据" + string(seek[:n]) + "\r\n")
					if e != nil || n < 7 || seek[0] != '[' {
						b.Write([]byte("[ro.config.marketing_name]: [null]\n"))
						return
					}
					b.Write(seek[:n])
				}
			case "host":
				// ADB服务端本身的（即host机器上的守护进程），而不是要转发给某个设备
				switch s[1] {
				case "devices":
					// 自动扫描
					go network()
				case "transport":
					if status == "OKAY" {
						continue
					}
				}
			}
			logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + fd + " AT 进入数据交互模式\r\n")
			go dd(f1, fd+" TX ")
			io.Copy(b, src)
			if s[1] == "kill" {
				os.Exit(0)
			}
			return
		}
	}()
	io.Copy(a, conn)
}

func dd(f2 io.ReadCloser, t string) {
	d := make([]byte, 1024*1024)
	for {
		i, e := f2.Read(d)
		if i > 0 {
			logger.Info(time.Now().Format("[2006-01-02 15:04:05.999]") + t + string(d[:i]) + "\r\n")
		}
		if e != nil {
			return
		}
	}
}

var lo int32

func network() {
	if !atomic.CompareAndSwapInt32(&lo, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&lo, 0)
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	var ip net.IP
	for _, addr := range addrs {
		// 检查地址类型是否为IP地址
		if v, ok := addr.(*net.IPNet); ok {
			if v.IP.To4() != nil && v.IP.IsPrivate() {
				once, _ := v.Mask.Size()
				if once != 24 || !strings.HasPrefix(v.IP.String(), "192.168.") {
					continue
				}
				ip = v.IP.To4()
				break
			}
		}
	}
	if ip == nil {
		return
	}

	list := make(chan struct{}, 50)
	sy := sync.WaitGroup{}
	ipList := []string{}
	lock := &sync.Mutex{}
	for i := 1; i < 255; i++ {
		if byte(i) == ip[3] {
			continue
		}
		list <- struct{}{}
		sy.Add(1)
		go func(num int) {
			defer sy.Done()
			defer func() { <-list }()
			ips := make(net.IP, 4)
			copy(ips, ip)
			ips[3] = byte(num)

			if icmpPing(ips.String()) {
				lock.Lock()
				ipList = append(ipList, ips.String())
				lock.Unlock()
			}
		}(i)
	}
	sy.Wait()

	for _, ii := range ipList {
		sy.Add(1)
		go func(s string) {
			sy.Done()
			if tcpPing(s, "5555") {
				connect(s)
			}
		}(ii)
	}
	sy.Wait()
}

func connect(ip string) {
	cmd := exec.Command(adb, "connect", ip)
	cmd.Run()
}

func restart() int {
	exec.Command(adb, "kill-server").Run()
	cmd := exec.Command(adb, "-P", strconv.Itoa(int(prot+1)), "start-server")
	//cmd.Stdin = os.Stdin
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return 1
	}
	return cmd.ProcessState.ExitCode()
}

// 使用go-ping库进行ICMP ping
func icmpPing(host string) bool {
	pinger, err := ping.NewPinger(host)
	if err != nil {
		return false
	}

	pinger.Count = 1
	pinger.Timeout = time.Millisecond * 500
	pinger.SetPrivileged(true)
	err = pinger.Run()
	if err != nil {
		return false
	}
	if pinger.Statistics().PacketsRecv > 0 {
		return true
	}
	return false
}

func tcpPing(host string, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}
