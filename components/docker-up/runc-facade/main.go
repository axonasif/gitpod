// Copyright (c) 2020 TypeFox GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License-AGPL.txt in the project root for license information.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	supervisor "github.com/gitpod-io/gitpod/supervisor/api"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

var (
	defaultOOMScoreAdj = 1000
)

const (
	cmdMountProc   = "mount-proc"
	cmdUnmountProc = "unmount-proc"
)

var log *logrus.Entry

func main() {
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	var runcDirect bool
	for _, arg := range os.Args {
		if arg == "-v" || arg == "--version" {
			runcDirect = true
			break
		}
	}
	if runcDirect {
		err := syscall.Exec("/usr/sbin/runc", os.Args, os.Environ())
		if err != nil {
			panic(err)
		}
	}

	var err error
	switch os.Args[0] {
	case cmdMountProc:
		err = mountProc()
	case cmdUnmountProc:
		err = unmountProc()
	default:
		err = runc()
	}
	if err != nil {
		log.WithError(err).Fatal("failed")
	}
}

func mountProc() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile("/tmp/runc-facade-mount", []byte(wd+"\n"+fmt.Sprint(os.Args)), 0644)
	if err != nil {
		return err
	}

	conn, err := grpc.Dial("unix:///var/run/ring1.sock", grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()

	client := supervisor.NewIsolationServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = client.MountProc(ctx, &supervisor.MountProcRequest{
		Pid:    -1,
		Target: filepath.Join(wd, "rootfs", "proc"),
	})
	if err != nil {
		return err
	}

	return nil
}

func unmountProc() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile("/tmp/runc-facade-unmount", []byte(wd), 0644)
	if err != nil {
		return err
	}

	return nil
}

func runc() error {
	fc, err := ioutil.ReadFile("config.json")
	if err != nil {
		return fmt.Errorf("cannot read config.json: %w", err)
	}

	var cfg specs.Spec
	err = json.Unmarshal(fc, &cfg)
	if err != nil {
		return fmt.Errorf("cannot decode config.json: %w", err)
	}

	cfg.Process.OOMScoreAdj = &defaultOOMScoreAdj
	replaceProcMount(&cfg)
	replaceSysMount(&cfg)
	err = addHooks(&cfg)
	if err != nil {
		return fmt.Errorf("canot add hooks: %w", err)
	}

	fc, err = json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot encode config.json: %w", err)
	}
	for _, fn := range []string{"config.json", "/tmp/debug.json"} {
		err = ioutil.WriteFile(fn, fc, 0644)
		if err != nil {
			return fmt.Errorf("cannot encode config.json: %w", err)
		}
	}

	err = syscall.Exec("/usr/sbin/runc", os.Args, os.Environ())
	if err != nil {
		return fmt.Errorf("exec runc: %w", err)
	}
	return nil
}

func replaceProcMount(cfg *specs.Spec) {
	var n int
	for _, m := range cfg.Mounts {
		if m.Destination == "/proc" {
			continue
		}

		cfg.Mounts[n] = m
		n++
	}

	cfg.Mounts = cfg.Mounts[:n]
	// TODO(cw): add daemon-mounted proc
	cfg.Mounts = append(cfg.Mounts, specs.Mount{
		Destination: "/proc",
		Options: []string{
			"rbind",
			"rprivate",
		},
		Source: "/proc",
		Type:   "bind",
	})
}

func replaceSysMount(cfg *specs.Spec) {
	var n int
	for _, m := range cfg.Mounts {
		if m.Destination == "/sys" {
			continue
		}

		cfg.Mounts[n] = m
		n++
	}

	cfg.Mounts = cfg.Mounts[:n]
	cfg.Mounts = append(cfg.Mounts, specs.Mount{
		Destination: "/sys",
		Options: []string{
			"rbind",
			"rprivate",
		},
		Source: "/sys",
		Type:   "bind",
	})
}

func addHooks(cfg *specs.Spec) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	cfg.Hooks.Prestart = append(cfg.Hooks.Prestart, specs.Hook{
		Path: self,
		Args: []string{cmdMountProc},
	})
	cfg.Hooks.Poststop = append(cfg.Hooks.Poststop, specs.Hook{
		Path: self,
		Args: []string{cmdUnmountProc},
	})
	return nil
}
