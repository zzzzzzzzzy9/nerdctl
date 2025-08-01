/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package container

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/containerd/console"
	"github.com/containerd/log"

	"github.com/containerd/nerdctl/v2/cmd/nerdctl/completion"
	"github.com/containerd/nerdctl/v2/pkg/annotations"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/clientutil"
	"github.com/containerd/nerdctl/v2/pkg/cmd/container"
	"github.com/containerd/nerdctl/v2/pkg/consoleutil"
	"github.com/containerd/nerdctl/v2/pkg/containerutil"
	"github.com/containerd/nerdctl/v2/pkg/defaults"
	"github.com/containerd/nerdctl/v2/pkg/errutil"
	"github.com/containerd/nerdctl/v2/pkg/labels"
	"github.com/containerd/nerdctl/v2/pkg/logging"
	"github.com/containerd/nerdctl/v2/pkg/netutil"
	"github.com/containerd/nerdctl/v2/pkg/signalutil"
	"github.com/containerd/nerdctl/v2/pkg/taskutil"
)

const (
	tiniInitBinary = "tini"
)

func RunCommand() *cobra.Command {
	shortHelp := "Run a command in a new container. Optionally specify \"ipfs://\" or \"ipns://\" scheme to pull image from IPFS."
	longHelp := shortHelp
	switch runtime.GOOS {
	case "windows":
		longHelp += "\n"
		longHelp += "WARNING: `nerdctl run` is experimental on Windows and currently broken (https://github.com/containerd/nerdctl/issues/28)"
	case "freebsd":
		longHelp += "\n"
		longHelp += "WARNING: `nerdctl run` is experimental on FreeBSD and currently requires `--net=none` (https://github.com/containerd/nerdctl/blob/main/docs/freebsd.md)"
	}
	var cmd = &cobra.Command{
		Use:               "run [flags] IMAGE [COMMAND] [ARG...]",
		Args:              cobra.MinimumNArgs(1),
		Short:             shortHelp,
		Long:              longHelp,
		RunE:              runAction,
		ValidArgsFunction: runShellComplete,
		SilenceUsage:      true,
		SilenceErrors:     true,
	}

	cmd.Flags().SetInterspersed(false)
	setCreateFlags(cmd)

	cmd.Flags().BoolP("detach", "d", false, "Run container in background and print container ID")
	cmd.Flags().StringSliceP("attach", "a", []string{}, "Attach STDIN, STDOUT, or STDERR")

	return cmd
}

func setCreateFlags(cmd *cobra.Command) {

	// No "-h" alias for "--help", because "-h" for "--hostname".
	cmd.Flags().Bool("help", false, "show help")

	cmd.Flags().BoolP("tty", "t", false, "Allocate a pseudo-TTY")
	cmd.Flags().Bool("sig-proxy", true, "Proxy received signals to the process (default true)")
	cmd.Flags().BoolP("interactive", "i", false, "Keep STDIN open even if not attached")
	cmd.Flags().String("restart", "no", `Restart policy to apply when a container exits (implemented values: "no"|"always|on-failure:n|unless-stopped")`)
	cmd.RegisterFlagCompletionFunc("restart", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"no", "always", "on-failure", "unless-stopped"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().Bool("rm", false, "Automatically remove the container when it exits")
	cmd.Flags().String("pull", "missing", `Pull image before running ("always"|"missing"|"never")`)
	cmd.Flags().BoolP("quiet", "q", false, "Suppress the pull output")
	cmd.RegisterFlagCompletionFunc("pull", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"always", "missing", "never"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().String("stop-signal", "SIGTERM", "Signal to stop a container")
	cmd.Flags().Int("stop-timeout", 0, "Timeout (in seconds) to stop a container")
	cmd.Flags().String("detach-keys", consoleutil.DefaultDetachKeys, "Override the default detach keys")

	// #region for init process
	cmd.Flags().Bool("init", false, "Run an init process inside the container, Default to use tini")
	cmd.Flags().String("init-binary", tiniInitBinary, "The custom binary to use as the init process")
	// #endregion

	// #region platform flags
	cmd.Flags().String("platform", "", "Set platform (e.g. \"amd64\", \"arm64\")") // not a slice, and there is no --all-platforms
	cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)
	// #endregion

	// #region network flags
	// network (net) is defined as StringSlice, not StringArray, to allow specifying "--network=cni1,cni2"
	cmd.Flags().StringSlice("network", []string{netutil.DefaultNetworkName}, `Connect a container to a network ("bridge"|"host"|"none"|"container:<container>"|"ns:<path>"|<CNI>)`)
	cmd.RegisterFlagCompletionFunc("network", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completion.NetworkNames(cmd, []string{})
	})
	cmd.Flags().StringSlice("net", []string{netutil.DefaultNetworkName}, `Connect a container to a network ("bridge"|"host"|"none"|"container:<container>"|"ns:<path>"|<CNI>)`)
	cmd.RegisterFlagCompletionFunc("net", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completion.NetworkNames(cmd, []string{})
	})
	// dns is defined as StringSlice, not StringArray, to allow specifying "--dns=1.1.1.1,8.8.8.8" (compatible with Podman)
	cmd.Flags().StringSlice("dns", nil, "Set custom DNS servers")
	cmd.Flags().StringSlice("dns-search", nil, "Set custom DNS search domains")
	// We allow for both "--dns-opt" and "--dns-option", although the latter is the recommended way.
	cmd.Flags().StringSlice("dns-opt", nil, "Set DNS options")
	cmd.Flags().StringSlice("dns-option", nil, "Set DNS options")
	// publish is defined as StringSlice, not StringArray, to allow specifying "--publish=80:80,443:443" (compatible with Podman)
	cmd.Flags().StringSliceP("publish", "p", nil, "Publish a container's port(s) to the host")
	cmd.Flags().String("ip", "", "IPv4 address to assign to the container")
	cmd.Flags().String("ip6", "", "IPv6 address to assign to the container")
	cmd.Flags().StringP("hostname", "h", "", "Container host name")
	cmd.Flags().String("domainname", "", "Container domain name")
	cmd.Flags().String("mac-address", "", "MAC address to assign to the container")
	// #endregion

	cmd.Flags().String("ipc", "", `IPC namespace to use ("host"|"private")`)
	cmd.RegisterFlagCompletionFunc("ipc", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"host", "private"}, cobra.ShellCompDirectiveNoFileComp
	})
	// #region cgroups, namespaces, and ulimits flags
	cmd.Flags().Float64("cpus", 0.0, "Number of CPUs")
	cmd.Flags().StringP("memory", "m", "", "Memory limit")
	cmd.Flags().String("memory-reservation", "", "Memory soft limit")
	cmd.Flags().String("memory-swap", "", "Swap limit equal to memory plus swap: '-1' to enable unlimited swap")
	cmd.Flags().Int64("memory-swappiness", -1, "Tune container memory swappiness (0 to 100) (default -1)")
	cmd.Flags().String("kernel-memory", "", "Kernel memory limit (deprecated)")
	cmd.Flags().Bool("oom-kill-disable", false, "Disable OOM Killer")
	cmd.Flags().Int("oom-score-adj", 0, "Tune container’s OOM preferences (-1000 to 1000, rootless: 100 to 1000)")
	cmd.Flags().String("pid", "", "PID namespace to use")
	cmd.Flags().String("uts", "", "UTS namespace to use")
	cmd.RegisterFlagCompletionFunc("pid", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"host"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().Int64("pids-limit", -1, "Tune container pids limit (set -1 for unlimited)")
	cmd.Flags().StringSlice("cgroup-conf", nil, "Configure cgroup v2 (key=value)")
	cmd.Flags().String("cgroupns", defaults.CgroupnsMode(), `Cgroup namespace to use, the default depends on the cgroup version ("host"|"private")`)
	cmd.Flags().String("cgroup-parent", "", "Optional parent cgroup for the container")
	cmd.RegisterFlagCompletionFunc("cgroupns", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"host", "private"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().String("cpuset-cpus", "", "CPUs in which to allow execution (0-3, 0,1)")
	cmd.Flags().String("cpuset-mems", "", "MEMs in which to allow execution (0-3, 0,1)")
	cmd.Flags().Uint64("cpu-shares", 0, "CPU shares (relative weight)")
	cmd.Flags().Int64("cpu-quota", -1, "Limit CPU CFS (Completely Fair Scheduler) quota")
	cmd.Flags().Uint64("cpu-period", 0, "Limit CPU CFS (Completely Fair Scheduler) period")
	cmd.Flags().Uint64("cpu-rt-period", 0, "Limit CPU real-time period in microseconds")
	cmd.Flags().Uint64("cpu-rt-runtime", 0, "Limit CPU real-time runtime in microseconds")
	// device is defined as StringSlice, not StringArray, to allow specifying "--device=DEV1,DEV2" (compatible with Podman)
	cmd.Flags().StringSlice("device", nil, "Add a host device to the container")
	// ulimit is defined as StringSlice, not StringArray, to allow specifying "--ulimit=ULIMIT1,ULIMIT2" (compatible with Podman)
	cmd.Flags().StringSlice("ulimit", nil, "Ulimit options")
	cmd.Flags().String("rdt-class", "", "Name of the RDT class (or CLOS) to associate the container with")
	// #endregion

	// #region blkio flags
	cmd.Flags().Uint16("blkio-weight", 0, "Block IO (relative weight), between 10 and 1000, or 0 to disable (default 0)")
	cmd.Flags().StringArray("blkio-weight-device", []string{}, "Block IO weight (relative device weight) (default [])")
	cmd.Flags().StringArray("device-read-bps", []string{}, "Limit read rate (bytes per second) from a device (default [])")
	cmd.Flags().StringArray("device-read-iops", []string{}, "Limit read rate (IO per second) from a device (default [])")
	cmd.Flags().StringArray("device-write-bps", []string{}, "Limit write rate (bytes per second) to a device (default [])")
	cmd.Flags().StringArray("device-write-iops", []string{}, "Limit write rate (IO per second) to a device (default [])")
	// #endregion

	// user flags
	cmd.Flags().StringP("user", "u", "", "Username or UID (format: <name|uid>[:<group|gid>])")
	cmd.Flags().String("umask", "", "Set the umask inside the container. Defaults to 0022")
	cmd.Flags().StringSlice("group-add", []string{}, "Add additional groups to join")

	// #region security flags
	cmd.Flags().StringArray("security-opt", []string{}, "Security options")
	cmd.RegisterFlagCompletionFunc("security-opt", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{
			"seccomp=", "seccomp=" + defaults.SeccompProfileName, "seccomp=unconfined",
			"apparmor=", "apparmor=" + defaults.AppArmorProfileName, "apparmor=unconfined",
			"no-new-privileges",
			"systempaths=unconfined",
			"privileged-without-host-devices"}, cobra.ShellCompDirectiveNoFileComp
	})
	// cap-add and cap-drop are defined as StringSlice, not StringArray, to allow specifying "--cap-add=CAP_SYS_ADMIN,CAP_NET_ADMIN" (compatible with Podman)
	cmd.Flags().StringSlice("cap-add", []string{}, "Add Linux capabilities")
	cmd.RegisterFlagCompletionFunc("cap-add", capShellComplete)
	cmd.Flags().StringSlice("cap-drop", []string{}, "Drop Linux capabilities")
	cmd.RegisterFlagCompletionFunc("cap-drop", capShellComplete)
	cmd.Flags().Bool("privileged", false, "Give extended privileges to this container")
	cmd.Flags().String("systemd", "false", "Allow running systemd in this container (default: false)")
	// #endregion

	// #region runtime flags
	cmd.Flags().String("runtime", defaults.Runtime, "Runtime to use for this container, e.g. \"crun\", or \"io.containerd.runsc.v1\"")
	// sysctl needs to be StringArray, not StringSlice, to prevent "foo=foo1,foo2" from being split to {"foo=foo1", "foo2"}
	cmd.Flags().StringArray("sysctl", nil, "Sysctl options")
	// gpus needs to be StringArray, not StringSlice, to prevent "capabilities=utility,device=DEV" from being split to {"capabilities=utility", "device=DEV"}
	cmd.Flags().StringArray("gpus", nil, "GPU devices to add to the container ('all' to pass all GPUs)")
	cmd.RegisterFlagCompletionFunc("gpus", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"all"}, cobra.ShellCompDirectiveNoFileComp
	})
	// #endregion

	// #region mount flags
	// volume needs to be StringArray, not StringSlice, to prevent "/foo:/foo:ro,Z" from being split to {"/foo:/foo:ro", "Z"}
	cmd.Flags().StringArrayP("volume", "v", nil, "Bind mount a volume")
	// tmpfs needs to be StringArray, not StringSlice, to prevent "/foo:size=64m,exec" from being split to {"/foo:size=64m", "exec"}
	cmd.Flags().StringArray("tmpfs", nil, "Mount a tmpfs directory")
	cmd.Flags().StringArray("mount", nil, "Attach a filesystem mount to the container")
	// volumes-from needs to be StringArray, not StringSlice, to prevent "id1,id2" from being split to {"id1", "id2"} (compatible with Docker)
	cmd.Flags().StringArray("volumes-from", nil, "Mount volumes from the specified container(s)")
	// #endregion

	// rootfs flags
	cmd.Flags().Bool("read-only", false, "Mount the container's root filesystem as read only")
	// rootfs flags (from Podman)
	cmd.Flags().Bool("rootfs", false, "The first argument is not an image but the rootfs to the exploded container")

	// Health check flags
	cmd.Flags().String("health-cmd", "", "Command to run to check health")
	cmd.Flags().Duration("health-interval", 0, "Time between running the check (default: 30s)")
	cmd.Flags().Duration("health-timeout", 0, "Maximum time to allow one check to run (default: 30s)")
	cmd.Flags().Int("health-retries", 0, "Consecutive failures needed to report unhealthy (default: 3)")
	cmd.Flags().Duration("health-start-period", 0, "Start period for the container to initialize before starting health-retries countdown")
	cmd.Flags().Duration("health-start-interval", 0, "Time between running the checks during the start period")
	cmd.Flags().Bool("no-healthcheck", false, "Disable any container-specified HEALTHCHECK")

	// #region env flags
	// entrypoint needs to be StringArray, not StringSlice, to prevent "FOO=foo1,foo2" from being split to {"FOO=foo1", "foo2"}
	// entrypoint StringArray is an internal implementation to support `nerdctl compose` entrypoint yaml filed with multiple strings
	// users are not expected to specify multiple --entrypoint flags manually.
	cmd.Flags().StringArray("entrypoint", nil, "Overwrite the default ENTRYPOINT of the image")
	cmd.Flags().StringP("workdir", "w", "", "Working directory inside the container")
	// env needs to be StringArray, not StringSlice, to prevent "FOO=foo1,foo2" from being split to {"FOO=foo1", "foo2"}
	cmd.Flags().StringArrayP("env", "e", nil, "Set environment variables")
	// add-host is defined as StringSlice, not StringArray, to allow specifying "--add-host=HOST1:IP1,HOST2:IP2" (compatible with Podman)
	cmd.Flags().StringSlice("add-host", nil, "Add a custom host-to-IP mapping (host:ip)")
	// env-file is defined as StringSlice, not StringArray, to allow specifying "--env-file=FILE1,FILE2" (compatible with Podman)
	cmd.Flags().StringSlice("env-file", nil, "Set environment variables from file")

	// #region metadata flags
	cmd.Flags().String("name", "", "Assign a name to the container")
	// label needs to be StringArray, not StringSlice, to prevent "foo=foo1,foo2" from being split to {"foo=foo1", "foo2"}
	cmd.Flags().StringArrayP("label", "l", nil, "Set metadata on container")
	// annotation needs to be StringArray, not StringSlice, to prevent "foo=foo1,foo2" from being split to {"foo=foo1", "foo2"}
	cmd.Flags().StringArray("annotation", nil, "Add an annotation to the container (passed through to the OCI runtime)")
	cmd.RegisterFlagCompletionFunc("annotation", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return annotations.ShellCompletions, cobra.ShellCompDirectiveNoFileComp
	})

	// label-file is defined as StringSlice, not StringArray, to allow specifying "--env-file=FILE1,FILE2" (compatible with Podman)
	cmd.Flags().StringSlice("label-file", nil, "Set metadata on container from file")
	cmd.Flags().String("cidfile", "", "Write the container ID to the file")
	// #endregion

	// #region logging flags
	// log-opt needs to be StringArray, not StringSlice, to prevent "env=os,customer" from being split to {"env=os", "customer"}
	cmd.Flags().String("log-driver", "json-file", "Logging driver for the container. Default is json-file. It also supports logURI (eg: --log-driver binary://<path>)")
	cmd.RegisterFlagCompletionFunc("log-driver", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return logging.Drivers(), cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().StringArray("log-opt", nil, "Log driver options")
	// #endregion

	// shared memory flags
	cmd.Flags().String("shm-size", "", "Size of /dev/shm")
	cmd.Flags().String("pidfile", "", "file path to write the task's pid")

	// #region verify flags
	cmd.Flags().String("verify", "none", "Verify the image (none|cosign|notation)")
	cmd.RegisterFlagCompletionFunc("verify", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"none", "cosign", "notation"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().String("cosign-key", "", "Path to the public key file, KMS, URI or Kubernetes Secret for --verify=cosign")
	cmd.Flags().String("cosign-certificate-identity", "", "The identity expected in a valid Fulcio certificate for --verify=cosign. Valid values include email address, DNS names, IP addresses, and URIs. Either --cosign-certificate-identity or --cosign-certificate-identity-regexp must be set for keyless flows")
	cmd.Flags().String("cosign-certificate-identity-regexp", "", "A regular expression alternative to --cosign-certificate-identity for --verify=cosign. Accepts the Go regular expression syntax described at https://golang.org/s/re2syntax. Either --cosign-certificate-identity or --cosign-certificate-identity-regexp must be set for keyless flows")
	cmd.Flags().String("cosign-certificate-oidc-issuer", "", "The OIDC issuer expected in a valid Fulcio certificate for --verify=cosign, e.g. https://token.actions.githubusercontent.com or https://oauth2.sigstore.dev/auth. Either --cosign-certificate-oidc-issuer or --cosign-certificate-oidc-issuer-regexp must be set for keyless flows")
	cmd.Flags().String("cosign-certificate-oidc-issuer-regexp", "", "A regular expression alternative to --certificate-oidc-issuer for --verify=cosign. Accepts the Go regular expression syntax described at https://golang.org/s/re2syntax. Either --cosign-certificate-oidc-issuer or --cosign-certificate-oidc-issuer-regexp must be set for keyless flows")
	// #endregion

	cmd.Flags().String("ipfs-address", "", "multiaddr of IPFS API (default uses $IPFS_PATH env variable if defined or local directory ~/.ipfs)")
	cmd.Flags().String("isolation", "default", "Specify isolation technology for container. On Linux the only valid value is default. Windows options are host, process and hyperv with process isolation as the default")
	cmd.RegisterFlagCompletionFunc("isolation", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if runtime.GOOS == "windows" {
			return []string{"default", "host", "process", "hyperv"}, cobra.ShellCompDirectiveNoFileComp
		}
		return []string{"default"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().String("userns", "", "Specify host to disable userns-remap")

}

func processCreateCommandFlagsInRun(cmd *cobra.Command) (types.ContainerCreateOptions, error) {
	opt, err := createOptions(cmd)
	if err != nil {
		return opt, err
	}

	opt.InRun = true

	opt.SigProxy, err = cmd.Flags().GetBool("sig-proxy")
	if err != nil {
		return opt, err
	}
	opt.Interactive, err = cmd.Flags().GetBool("interactive")
	if err != nil {
		return opt, err
	}
	opt.Detach, err = cmd.Flags().GetBool("detach")
	if err != nil {
		return opt, err
	}
	opt.DetachKeys, err = cmd.Flags().GetString("detach-keys")
	if err != nil {
		return opt, err
	}
	opt.Attach, err = cmd.Flags().GetStringSlice("attach")
	if err != nil {
		return opt, err
	}

	validAttachFlag := true
	for i, str := range opt.Attach {
		opt.Attach[i] = strings.ToUpper(str)

		if opt.Attach[i] != "STDIN" && opt.Attach[i] != "STDOUT" && opt.Attach[i] != "STDERR" {
			validAttachFlag = false
		}
	}
	if !validAttachFlag {
		return opt, fmt.Errorf("invalid stream specified with -a flag. Valid streams are STDIN, STDOUT, and STDERR")
	}

	return opt, nil
}

// runAction is heavily based on ctr implementation:
// https://github.com/containerd/containerd/blob/v1.4.3/cmd/ctr/commands/run/run.go
func runAction(cmd *cobra.Command, args []string) error {
	var isDetached bool

	createOpt, err := processCreateCommandFlagsInRun(cmd)
	if err != nil {
		return err
	}

	client, ctx, cancel, err := clientutil.NewClientWithPlatform(cmd.Context(), createOpt.GOptions.Namespace, createOpt.GOptions.Address, createOpt.Platform)
	if err != nil {
		return err
	}
	defer cancel()

	if createOpt.Rm && createOpt.Detach {
		return errors.New("flags -d and --rm cannot be specified together")
	}

	if len(createOpt.Attach) > 0 && createOpt.Detach {
		return errors.New("flags -d and -a cannot be specified together")
	}

	netFlags, err := loadNetworkFlags(cmd, createOpt.GOptions)
	if err != nil {
		return fmt.Errorf("failed to load networking flags: %w", err)
	}

	netManager, err := containerutil.NewNetworkingOptionsManager(createOpt.GOptions, netFlags, client)
	if err != nil {
		return err
	}

	c, gc, err := container.Create(ctx, client, args, netManager, createOpt)
	if err != nil {
		if gc != nil {
			defer gc()
		}
		return err
	}
	// defer setting `nerdctl/error` label in case of error
	defer func() {
		if err != nil {
			containerutil.UpdateErrorLabel(ctx, c, err)
		}
	}()

	id := c.ID()
	if createOpt.Rm {
		defer func() {
			if isDetached {
				return
			}
			if err := container.RemoveContainer(ctx, c, createOpt.GOptions, true, true, client); err != nil {
				log.L.WithError(err).Warnf("failed to remove container %s", id)
			}
		}()
	}

	var con console.Console
	if createOpt.TTY && !createOpt.Detach {
		con, err = consoleutil.Current()
		if err != nil {
			return err
		}
		defer con.Reset()
		if _, err := term.MakeRaw(int(con.Fd())); err != nil {
			return err
		}
	}

	lab, err := c.Labels(ctx)
	if err != nil {
		return err
	}
	logURI := lab[labels.LogURI]
	detachC := make(chan struct{})
	task, err := taskutil.NewTask(ctx, client, c, createOpt.Attach, createOpt.Interactive, createOpt.TTY, createOpt.Detach,
		con, logURI, createOpt.DetachKeys, createOpt.GOptions.Namespace, detachC)
	if err != nil {
		return err
	}
	if err := task.Start(ctx); err != nil {
		return err
	}

	if createOpt.Detach {
		fmt.Fprintln(createOpt.Stdout, id)
		return nil
	}
	if createOpt.TTY {
		if err := consoleutil.HandleConsoleResize(ctx, task, con); err != nil {
			log.L.WithError(err).Error("console resize")
		}
	} else {
		if createOpt.SigProxy {
			sigC := signalutil.ForwardAllSignals(ctx, task)
			defer signalutil.StopCatch(sigC)
		}
	}

	statusC, err := task.Wait(ctx)
	if err != nil {
		return err
	}
	select {
	// io.Wait() would return when either 1) the user detaches from the container OR 2) the container is about to exit.
	//
	// If we replace the `select` block with io.Wait() and
	// directly use task.Status() to check the status of the container after io.Wait() returns,
	// it can still be running even though the container is about to exit (somehow especially for Windows).
	//
	// As a result, we need a separate detachC to distinguish from the 2 cases mentioned above.
	case <-detachC:
		io := task.IO()
		if io == nil {
			return errors.New("got a nil IO from the task")
		}
		io.Wait()
		isDetached = true
	case status := <-statusC:
		if createOpt.Rm {
			if _, taskDeleteErr := task.Delete(ctx); taskDeleteErr != nil {
				log.L.Error(taskDeleteErr)
			}
		}
		code, _, err := status.Result()
		if err != nil {
			return err
		}
		if code != 0 {
			return errutil.NewExitCoderErr(int(code))
		}
	}
	return nil
}

func runShellComplete(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completion.ImageNames(cmd)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
