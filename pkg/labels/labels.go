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

// Package labels defines labels that are set to containerd containers as labels.
// The labels defined in this package are also passed to OCI containers as annotations.
package labels

const (
	// Prefix is the common prefix of nerdctl labels
	Prefix = "nerdctl/"

	// Namespace is the containerd namespace such as "default", "k8s.io"
	Namespace = Prefix + "namespace"

	// Name is a human-friendly name.
	// WARNING: multiple containers may have same the name label
	Name = Prefix + "name"

	//Compose Project Name
	ComposeProject = "com.docker.compose.project"

	//Compose Service Name
	ComposeService = "com.docker.compose.service"

	//Compose Network Name
	ComposeNetwork = "com.docker.compose.network"

	//Compose Volume Name
	ComposeVolume = "com.docker.compose.volume"

	// Hostname
	Hostname = Prefix + "hostname"

	// Domainname
	Domainname = Prefix + "domainname"

	// ExtraHosts are HostIPs to appended to /etc/hosts
	ExtraHosts = Prefix + "extraHosts"

	// StateDir is "/var/lib/nerdctl/<ADDRHASH>/containers/<NAMESPACE>/<ID>"
	StateDir = Prefix + "state-dir"

	// Networks is a JSON-marshalled string of []string, e.g. []string{"bridge"}.
	// Currently, the length of the slice must be 1.
	Networks = Prefix + "networks"

	// DEPRECATED : https://github.com/containerd/nerdctl/pull/4290
	// Ports is a JSON-marshalled string of []cni.PortMapping .
	Ports = Prefix + "ports"

	// IPAddress is the static IP address of the container assigned by the user
	IPAddress = Prefix + "ip"

	// IP6Address is the static IP6 address of the container assigned by the user
	IP6Address = Prefix + "ip6"

	// LogURI is the log URI
	LogURI = Prefix + "log-uri"

	// PIDFile is the `nerdctl run --pidfile`
	// (CLI flag is "pidfile", not "pid-file", for Podman compatibility)
	PIDFile = Prefix + "pid-file"

	// AnonymousVolumes is a JSON-marshalled string of []string
	AnonymousVolumes = Prefix + "anonymous-volumes"

	// Platform is the normalized platform string like "linux/ppc64le".
	Platform = Prefix + "platform"

	// Mounts is the mount points for the container.
	Mounts = Prefix + "mounts"

	// StopTimeout is seconds to wait for stop a container.
	StopTimeout = Prefix + "stop-timeout"

	MACAddress = Prefix + "mac-address"

	// PIDContainer is the `nerdctl run --pid` for restarting
	PIDContainer = Prefix + "pid-container"

	// IPC is the `nerectl run --ipc` for restrating
	// IPC indicates ipc victim container.
	IPC = Prefix + "ipc"

	// Error encapsulates a container human-readable string
	// that describes container error.
	Error = Prefix + "error"

	// NerdctlDefaultNetwork indicates whether a network is the default network
	// created and owned by Nerdctl.
	// Boolean value which can be parsed with strconv.ParseBool() is required.
	// (like "nerdctl/default-network=true" or "nerdctl/default-network=false")
	NerdctlDefaultNetwork = Prefix + "default-network"

	// ContainerAutoRemove is to check whether the --rm option is specified.
	ContainerAutoRemove = Prefix + "auto-remove"

	// LogConfig defines the logging configuration passed to the container
	LogConfig = Prefix + "log-config"

	// HostConfigLabel sets the dockercompat host config values
	HostConfigLabel = Prefix + "host-config"

	// DNSSettings sets the dockercompat DNS config values
	DNSSetting = Prefix + "dns"

	// User is the username of the container
	User = Prefix + "user"

	// HealthCheck stores the health check configuration used to run health checks on the container
	HealthCheck = Prefix + "healthcheck"

	// HealthState stores the current health state (status and failing streak).
	HealthState = Prefix + "healthstate"
)
