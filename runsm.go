/*
 * I B A U K   -   S C O R E M A S T E R
 *
 * I'm responsible for initiating and maintaining a PHP CGI server.
 *
 * PHP running in CGI mode can suffer from memory leaks and other flaws so it's
 * desirable to restart the executable periodically.
 *
 * I am written for readability rather than efficiency, please keep me that way.
 *
 *
 * Copyright (c) 2022 Bob Stammers
 *
 *
 * This file is part of IBAUK-SCOREMASTER.
 *
 * IBAUK-SCOREMASTER is free software: you can redistribute it and/or modify
 * it under the terms of the MIT License
 *
 * IBAUK-SCOREMASTER is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * MIT License for more details.
 *
 *
 *	2019-09-21	First Go release
 *	2019-09-29	Check/report changed IP
 *	2019-11-05	Linux setup; webserver/PHP already available
 *	2020-02-19	Apple Mac setup
 *	2020-06-29	Window title
 *	2020-12-04	Bumped to v2.7, Caddy v2
 *	2021-06-24	Integrated EBC
 *	2022-03-29	Suppress IP monitoring by default
 *  2022-06-06	Include tzdata
 *
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/pkg/browser"
)

const myPROGTITLE = "ScoreMaster Server v3.1.1"
const myWINTITLE = "IBA ScoreMaster"

var phpcgi = filepath.Join("php", "php-cgi")
var phpdbg = ""

var debug = flag.Bool("debug", false, "Run in PHP debug mode")
var port = flag.String("port", "80", "Webserver port specification")
var alternateWebPort = flag.String("altport", "2015", "Alternate webserver port")
var ipspec = flag.String("ip", "*", "Webserver IP specification")
var spawnInterval = flag.Int("respawn", 60, "Number of minutes before restarting PHP server")
var nolocal = flag.Bool("nolocal", false, "Don't start a web browser on the host machine")
var ipWatch = flag.Bool("watch", false, "Monitor/report IP address changes")

const cgiport = "127.0.0.1:9000"
const smCaddyFolder = "caddy"
const starturl = "http://localhost"

var shuttingDown bool = false

type logWriter struct {
}

func (writer logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().UTC().Format("2006-01-02 15:04:05") + " " + string(bytes))
}

func init() {

	log.SetFlags(0)
	log.SetOutput(new(logWriter))

	os := runtime.GOOS
	switch os {
	case "darwin": // Apple

		phpdbg = "/usr/bin/php"
		setMyWindowTitle(myWINTITLE)

	case "linux":

		phpdbg = "/usr/bin/php"
		setMyWindowTitle(myWINTITLE)

	case "windows":

		phpdbg = "\\php\\php"
		setMyWindowTitle(myWINTITLE)

	default:
		// freebsd, openbsd,
		// plan9, ...
	}
}

func main() {

	var cancelCaddy context.CancelFunc = nil
	var cancelEBCFetch context.CancelFunc = nil
	var serverIP net.IP

	fmt.Printf("\n%s\t\t%s\n", "Iron Butt Association UK", "webmaster@ironbutt.co.uk")
	fmt.Printf("\n%s\n\n", myPROGTITLE)

	setupRun()

	if *ipWatch {
		serverIP = getOutboundIP()
		fmt.Printf("%s IPv4 = %s\n", timestamp(), serverIP)
	}

	if *debug && phpdbg != "" {
		debugPHP()
	} else {
		cancelCaddy = runCaddy()
		go runPHP()
	}

	cancelEBCFetch = runEBCFetch()

	if !*nolocal {
		showInvite()
	}

	if *debug {
		fmt.Printf("%s quitting\n\n", timestamp())
		os.Exit(0)
	}

	if *ipWatch {
		go monitorIP(serverIP)
	}

	// Now just kill time and wait for someone to kill me
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		sig := <-sigs
		fmt.Printf("%s\n", timestamp())
		fmt.Printf("%s %v\n", timestamp(), sig)
		fmt.Printf("%s\n", timestamp())
		done <- true
	}()
	<-done
	shuttingDown = true
	if cancelCaddy != nil {
		fmt.Printf("%s ending Caddy\n", timestamp())
		killCaddy()
	}
	if cancelEBCFetch != nil {
		fmt.Printf("%s ending EBCFetch\n", timestamp())
		killEBCFetch()
	}
	fmt.Printf("%s quitting\n", timestamp())
}

func monitorIP(serverIP net.IP) {

	for {
		time.Sleep(1 * time.Minute)
		myIP := getOutboundIP()
		if !myIP.Equal(serverIP) {
			serverIP = myIP
			fmt.Printf("%s IPv4 = %s\n", timestamp(), serverIP)
		}
	}

}

func showInvite() {

	time.Sleep(5 * time.Second)
	fmt.Println(timestamp() + " presenting " + starturl + ":" + *port)
	browser.OpenURL(starturl + ":" + *port)

}

func debugPHP() {
	// This runs PHP as a local, single user, webserver as an aid to debugging or for
	// very lightweight usage

	if phpdbg == "" {
		return
	}
	fmt.Println(timestamp() + " debugging PHP")
	cmd := exec.Command("cmd", "/C", "start", "/min", phpdbg, "-S", "127.0.0.1:"+*port, "-t", "sm", "-c", filepath.Join("php", "php.ini"))
	cmd.Env = append(os.Environ(), "PHP_FCGI_MAX_REQUESTS=0")
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

}

func execPHP() {
	// This runs PHP as a background service to an external webserver
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*spawnInterval)*time.Minute)
	defer cancel()
	if err := exec.CommandContext(ctx, phpcgi, "-b", cgiport).Run(); err != nil {
		//fmt.Println(phpcgi+" <=== ")
		log.Printf("PHP %v\n", err)
	}
}

func getOutboundIP() net.IP {
	udp := "udp"
	ip := "8.8.8.8:80" // Google public DNS

	//	udp := "udp6"
	//	ip := "[2a03:2880:f003:c07:face:b00c::2]:80"	// Facebook public DNS

	conn, err := net.Dial(udp, ip)
	if err != nil {
		log.Print(err)
		return net.IPv4(127, 0, 0, 1)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}

func timestamp() string {

	var t = time.Now()
	return t.Format("2006-01-02 15:04:05")

}

func runPHP() {

	cgi := strings.Split(cgiport, ":")
	if !rawPortAvail(cgi[1]) {
		fmt.Println(timestamp() + " PHP [" + cgi[1] + "] already listening")
		return
	}
	os.Setenv("PHP_FCGI_MAX_REQUESTS", "0") // PHP defaults to die after 500 requests so disable that
	x := "spawning"
	for !shuttingDown {
		fmt.Printf("%s %s PHP\n", timestamp(), x)
		x = "respawning"
		execPHP()
	}

}

func runCaddy() context.CancelFunc {

	// If IP is not wildcard then assume that grownup has checked
	if *ipspec == "*" {
		if !rawPortAvail(*port) {
			fmt.Println(timestamp() + " service port " + *port + " already served")
			return nil
		}
		if !testWebPort(*port) {
			if *port != *alternateWebPort && testWebPort(*alternateWebPort) {
				*port = *alternateWebPort
			} else {
				fmt.Println(timestamp() + " switching to alternate port " + *port)
			}
		}
	}
	fmt.Printf(timestamp() + " serving on " + *ipspec + ":" + *port + "\n")
	// Create the conf file
	cp := filepath.Join(smCaddyFolder, "caddyfile")

	// ep := filepath.Join(smCaddyFolder, "error.log")
	f, err := os.Create(cp)
	if err != nil {
		log.Fatal(err)
	}
	f.WriteString("{\nhttp_port " + *port + "\n}\n")
	f.WriteString(*ipspec + ":" + *port + "\n")
	f.WriteString("file_server\n")
	f.WriteString("root sm\n")
	//f.WriteString("log {\noutput file " + ep + "\n}\n")
	f.WriteString("php_fastcgi " + cgiport + "\n")
	f.Close()

	// Now run Caddy
	ctx, cancel := context.WithCancel(context.Background())
	//defer cancel()
	fp := filepath.Join(smCaddyFolder, "caddy")

	if err := exec.CommandContext(ctx, fp, "start", "--config", cp, "--adapter", "caddyfile").Run(); err != nil {
		log.Fatal(err)
	}
	return cancel

}

func runEBCFetch() context.CancelFunc {

	ctx, cancel := context.WithCancel(context.TODO())
	fp := filepath.Join(smCaddyFolder, "ebcfetch")

	fmt.Printf("%s spawning %s\n", timestamp(), fp)

	cmd := exec.CommandContext(ctx, fp)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Printf("%s %s unspawned %v \n", timestamp(), fp, err)

		log.Fatal(err)
	}
	fmt.Printf("%s %s spawned\n", timestamp(), fp)
	return cancel

}

func killCaddy() {

	fp := filepath.Join(smCaddyFolder, "caddy")
	cmd := exec.Command(fp, "stop")
	cmd.Start()

}

func killEBCFetch() {

}

func setupRun() {

	args := os.Args

	// Change to the folder containing this executable
	dir := filepath.Dir(args[0])
	os.Chdir(dir)
	flag.Parse()
	if !*debug && runtime.GOOS == "windows" {
		filename := filepath.Base(os.Args[0])
		*debug = strings.Contains(filename, "debug")
	}
}

func rawPortAvail(port string) bool {

	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", port), timeout)
	if err != nil {
		return true
	}
	if conn != nil {
		defer conn.Close()
		return false
	}
	return true
}

func testWebPort(port string) bool {
	/*
	 * Used to trap port access not permitted errors
	 *
	 */
	ln, err := net.Listen("tcp", ":"+port)

	if err != nil {
		fmt.Printf("%s port %s NOT AVAILABLE: %s\n", timestamp(), port, err)
		return false
	}

	ln.Close()
	fmt.Printf("%s port %s available\n", timestamp(), port)
	return true
}
