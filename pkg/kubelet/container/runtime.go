/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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
	"fmt"
	"io"
	"strings"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/types"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/volume"
)

type Version interface {
	// Compare compares two versions of the runtime. On success it returns -1
	// if the version is less than the other, 1 if it is greater than the other,
	// or 0 if they are equal.
	Compare(other string) (int, error)
	// String returns a string that represents the version.
	String() string
}

// ImageSpec is an internal representation of an image.  Currently, it wraps the
// value of a Container's Image field, but in the future it will include more detailed
// information about the different image types.
type ImageSpec struct {
	Image string
}

// Runtime interface defines the interfaces that should be implemented
// by a container runtime.
type Runtime interface {
	// Version returns the version information of the container runtime.
	Version() (Version, error)
	// GetPods returns a list containers group by pods. The boolean parameter
	// specifies whether the runtime returns all containers including those already
	// exited and dead containers (used for garbage collection).
	GetPods(all bool) ([]*Pod, error)
	// Syncs the running pod into the desired pod.
	SyncPod(pod *api.Pod, runningPod Pod, podStatus api.PodStatus) error
	// KillPod kills all the containers of a pod.
	KillPod(pod Pod) error
	// GetPodStatus retrieves the status of the pod, including the information of
	// all containers in the pod.
	GetPodStatus(*api.Pod) (*api.PodStatus, error)
	// TODO(vmarmol): Merge RunInContainer and ExecInContainer.
	// Runs the command in the container of the specified pod using nsinit.
	// TODO(yifan): Use strong type for containerID.
	RunInContainer(containerID string, cmd []string) ([]byte, error)
	// Runs the command in the container of the specified pod using nsenter.
	// Attaches the processes stdin, stdout, and stderr. Optionally uses a
	// tty.
	// TODO(yifan): Use strong type for containerID.
	ExecInContainer(containerID string, cmd []string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool) error
	// Forward the specified port from the specified pod to the stream.
	PortForward(pod *Pod, port uint16, stream io.ReadWriteCloser) error
	// PullImage pulls an image from the network to local storage using the supplied
	// secrets if necessary.
	PullImage(image ImageSpec, secrets []api.Secret) error
	// IsImagePresent checks whether the container image is already in the local storage.
	IsImagePresent(image ImageSpec) (bool, error)
	// Gets all images currently on the machine.
	ListImages() ([]Image, error)
	// Removes the specified image.
	RemoveImage(image ImageSpec) error
	// TODO(vmarmol): Unify pod and containerID args.
	// GetContainerLogs returns logs of a specific container. By
	// default, it returns a snapshot of the container log. Set 'follow' to true to
	// stream the log. Set 'follow' to false and specify the number of lines (e.g.
	// "100" or "all") to tail the log.
	GetContainerLogs(pod *api.Pod, containerID, tail string, follow bool, stdout, stderr io.Writer) (err error)
}

// Customizable hooks injected into container runtimes.
type RuntimeHooks interface {
	// Determines whether the runtime should pull the specified container's image.
	ShouldPullImage(pod *api.Pod, container *api.Container, imagePresent bool) bool

	// Runs after an image is pulled reporting its status. Error may be nil
	// for a successful pull.
	ReportImagePull(pod *api.Pod, container *api.Container, err error)
}

// Pod is a group of containers, with the status of the pod.
type Pod struct {
	// The ID of the pod, which can be used to retrieve a particular pod
	// from the pod list returned by GetPods().
	ID types.UID
	// The name and namespace of the pod, which is readable by human.
	Name      string
	Namespace string
	// List of containers that belongs to this pod. It may contain only
	// running containers, or mixed with dead ones (when GetPods(true)).
	Containers []*Container
	// The status of the pod.
	// TODO(yifan): Inspect and get the statuses for all pods can be expensive,
	// maybe we want to get one pod's status at a time (e.g. GetPodStatus()
	// for the particular pod after we GetPods()).
	Status api.PodStatus
}

// ContainerID is a type that identifies a container.
type ContainerID struct {
	// The type of the container runtime. e.g. 'docker', 'rkt'.
	Type string
	// The identification of the container, this is comsumable by
	// the underlying container runtime. (Note that the container
	// runtime interface still takes the whole struct as input).
	ID string
}

func BuildContainerID(typ, ID string) ContainerID {
	return ContainerID{Type: typ, ID: ID}
}

func (c *ContainerID) ParseString(data string) error {
	// Trim the quotes and split the type and ID.
	parts := strings.Split(strings.Trim(data, "\""), "://")
	if len(parts) != 2 {
		return fmt.Errorf("invalid container ID: %q", data)
	}
	c.Type, c.ID = parts[0], parts[1]
	return nil
}

func (c *ContainerID) String() string {
	return fmt.Sprintf("%s://%s", c.Type, c.ID)
}

func (c *ContainerID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", c.String())), nil
}

func (c *ContainerID) UnmarshalJSON(data []byte) error {
	return c.ParseString(string(data))
}

// Container provides the runtime information for a container, such as ID, hash,
// status of the container.
type Container struct {
	// The ID of the container, used by the container runtime to identify
	// a container.
	ID types.UID
	// The name of the container, which should be the same as specified by
	// api.Container.
	Name string
	// The image name of the container.
	Image string
	// Hash of the container, used for comparison. Optional for containers
	// not managed by kubelet.
	Hash uint64
	// The timestamp of the creation time of the container.
	// TODO(yifan): Consider to move it to api.ContainerStatus.
	Created int64
}

// Basic information about a container image.
type Image struct {
	// ID of the image.
	ID string
	// Other names by which this image is known.
	Tags []string
	// The size of the image in bytes.
	Size int64
}

// RunContainerOptions specify the options which are necessary for running containers
type RunContainerOptions struct {
	// The environment variables, they are in the form of 'key=value'.
	Envs []string
	// The mounts for the containers, they are in the form of:
	// 'hostPath:containerPath', or
	// 'hostPath:containerPath:ro', if the path read only.
	Binds []string
	// If the container has specified the TerminationMessagePath, then
	// this directory will be used to create and mount the log file to
	// container.TerminationMessagePath
	PodContainerDir string
	// The list of DNS servers for the container to use.
	DNS []string
	// The list of DNS search domains.
	DNSSearch []string
	// Docker namespace identifiers(currently we have 'NetMode' and 'IpcMode'.
	// These are for docker to attach a container in a pod to the pod infra
	// container's namespace.
	// TODO(yifan): Remove these after we pushed the pod infra container logic
	// into docker's container runtime.
	NetMode string
	IpcMode string
	// The parent cgroup to pass to Docker
	CgroupParent string
}

type VolumeMap map[string]volume.Volume

type Pods []*Pod

// FindPodByID finds and returns a pod in the pod list by UID. It will return an empty pod
// if not found.
func (p Pods) FindPodByID(podUID types.UID) Pod {
	for i := range p {
		if p[i].ID == podUID {
			return *p[i]
		}
	}
	return Pod{}
}

// FindPodByFullName finds and returns a pod in the pod list by the full name.
// It will return an empty pod if not found.
func (p Pods) FindPodByFullName(podFullName string) Pod {
	for i := range p {
		if BuildPodFullName(p[i].Name, p[i].Namespace) == podFullName {
			return *p[i]
		}
	}
	return Pod{}
}

// FindPod combines FindPodByID and FindPodByFullName, it finds and returns a pod in the
// pod list either by the full name or the pod ID. It will return an empty pod
// if not found.
func (p Pods) FindPod(podFullName string, podUID types.UID) Pod {
	if len(podFullName) > 0 {
		return p.FindPodByFullName(podFullName)
	}
	return p.FindPodByID(podUID)
}

// FindContainerByName returns a container in the pod with the given name.
// When there are multiple containers with the same name, the first match will
// be returned.
func (p *Pod) FindContainerByName(containerName string) *Container {
	for _, c := range p.Containers {
		if c.Name == containerName {
			return c
		}
	}
	return nil
}

// GetPodFullName returns a name that uniquely identifies a pod.
func GetPodFullName(pod *api.Pod) string {
	// Use underscore as the delimiter because it is not allowed in pod name
	// (DNS subdomain format), while allowed in the container name format.
	return fmt.Sprintf("%s_%s", pod.Name, pod.Namespace)
}

// Build the pod full name from pod name and namespace.
func BuildPodFullName(name, namespace string) string {
	return name + "_" + namespace
}

// Parse the pod full name.
func ParsePodFullName(podFullName string) (string, string, error) {
	parts := strings.Split(podFullName, "_")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("failed to parse the pod full name %q", podFullName)
	}
	return parts[0], parts[1], nil
}
