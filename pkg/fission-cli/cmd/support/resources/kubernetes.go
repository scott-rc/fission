/*
Copyright 2018 The Fission Authors.

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

package resources

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/fission/fission/pkg/fission-cli/console"
)

const (
	KubernetesService    = "Service"
	KubernetesDeployment = "Deployment"
	KubernetesPod        = "Pod"
	KubernetesHPA        = "HPA"
	KubernetesNode       = "Node"
	KubernetesDaemonSet  = "DaemonSet"
)

// Kubernetes Version
type KubernetesVersion struct {
	client kubernetes.Interface
}

func NewKubernetesVersion(clientset kubernetes.Interface) Resource {
	return KubernetesVersion{client: clientset}
}

func (res KubernetesVersion) Dump(ctx context.Context, dumpDir string) {
	serverVer, err := res.client.Discovery().ServerVersion()
	if err != nil {
		console.Error(fmt.Sprintf("Error setting up kubernetes client: %v", err))
		return
	}

	file := fmt.Sprintf("%v/%v", dumpDir, "kubernetes-version.txt")
	writeToFile(file, serverVer)
}

// Kubernetes Object Dumper
type KubernetesObjectDumper struct {
	client   kubernetes.Interface
	objType  string
	selector string
}

func NewKubernetesObjectDumper(clientset kubernetes.Interface, objType string, selector string) Resource {
	return KubernetesObjectDumper{
		client:   clientset,
		objType:  objType,
		selector: selector,
	}
}

func (res KubernetesObjectDumper) Dump(ctx context.Context, dumpDir string) {
	switch res.objType {
	case KubernetesService:
		objs, err := res.client.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: res.selector})
		if err != nil {
			console.Error(fmt.Sprintf("Error getting %v list with selector %v: %v", res.objType, res.selector, err))
			return
		}

		for _, item := range objs.Items {
			item = serviceClean(item)
			f := getFileName(dumpDir, item.ObjectMeta)
			writeToFile(f, item)
		}

	case KubernetesDeployment:
		objs, err := res.client.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: res.selector})
		if err != nil {
			console.Error(fmt.Sprintf("Error getting %v list with selector %v: %v", res.objType, res.selector, err))
			return
		}

		for _, item := range objs.Items {
			f := getFileName(dumpDir, item.ObjectMeta)
			writeToFile(f, item)
		}

	case KubernetesPod:
		objs, err := res.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: res.selector})
		if err != nil {
			console.Error(fmt.Sprintf("Error getting %v list with selector %v: %v", res.objType, res.selector, err))
			return
		}

		for _, item := range objs.Items {
			f := getFileName(dumpDir, item.ObjectMeta)
			writeToFile(f, item)
		}

	case KubernetesHPA:
		objs, err := res.client.AutoscalingV2beta2().HorizontalPodAutoscalers(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: res.selector})
		if err != nil {
			console.Error(fmt.Sprintf("Error getting %v list with selector %v: %v", res.objType, res.selector, err))
			return
		}

		for _, item := range objs.Items {
			f := getFileName(dumpDir, item.ObjectMeta)
			writeToFile(f, item)
		}

	case KubernetesDaemonSet:
		objs, err := res.client.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: res.selector})
		if err != nil {
			console.Error(fmt.Sprintf("Error getting %v list with selector %v: %v", res.objType, res.selector, err))
			return
		}

		for _, item := range objs.Items {
			f := getFileName(dumpDir, item.ObjectMeta)
			writeToFile(f, item)
		}

	case KubernetesNode:
		objs, err := res.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: res.selector})
		if err != nil {
			console.Error(fmt.Sprintf("Error getting %v list with selector %v: %v", res.objType, res.selector, err))
			return
		}

		for _, item := range objs.Items {
			item = nodeClean(item)
			// Node doesn't have namespace value, use name here
			f := filepath.Clean(fmt.Sprintf("%v/%v", dumpDir, item.Name))
			writeToFile(f, item)
		}

	default:
		console.Error(fmt.Sprintf("Unknown type: %v", res.objType))
		return
	}
}

// serviceClean remove sensitive data(e.g. public IP, external name) from service objects
func serviceClean(svc corev1.Service) corev1.Service {
	svc.Spec.ExternalIPs = []string{}
	svc.Spec.LoadBalancerIP = ""
	svc.Spec.LoadBalancerSourceRanges = []string{}
	svc.Spec.ExternalName = ""
	svc.Status.LoadBalancer = corev1.LoadBalancerStatus{}
	return svc
}

func nodeClean(node corev1.Node) corev1.Node {

	var nodeAddresses []corev1.NodeAddress
	for _, address := range node.Status.Addresses {
		// use whitelist to filter the necessary information for debugging
		if address.Type == "InternalIP" || address.Type == "Hostname" {
			nodeAddresses = append(nodeAddresses, address)
		}
	}
	node.Status.Addresses = nodeAddresses

	return node
}

type KubernetesPodLogDumper struct {
	client        kubernetes.Interface
	labelSelector string
}

func NewKubernetesPodLogDumper(clientset kubernetes.Interface, selector string) Resource {
	return KubernetesPodLogDumper{
		client:        clientset,
		labelSelector: selector,
	}
}

func (res KubernetesPodLogDumper) Dump(ctx context.Context, dumpDir string) {
	l, err := res.client.CoreV1().
		Pods(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{LabelSelector: res.labelSelector})
	if err != nil {
		console.Error(fmt.Sprintf("Error getting controller list: %v", err))
		return
	}

	wg := &sync.WaitGroup{}

	for _, p := range l.Items {
		wg.Add(1)

		go func(pod corev1.Pod) {
			defer wg.Done()

			// dump logs from each containers
			for _, container := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
				req := res.client.CoreV1().Pods(pod.Namespace).
					GetLogs(pod.Name, &corev1.PodLogOptions{Container: container.Name})

				stream, err := req.Stream(ctx)
				if err != nil {
					console.Error(fmt.Sprintf("Error streaming logs for pod %v: %v", pod.Name, err))
					return
				}

				reader := bufio.NewReader(stream)
				var buffer bytes.Buffer

				for {
					line, _, err := reader.ReadLine()
					if err != nil {
						if err == io.EOF {
							stream.Close()
							break
						}
						console.Error(fmt.Sprintf("Error reading logs from buffer: %v", err))
						return
					}

					_, err = buffer.WriteString(string(line) + "\n")
					if err != nil {
						console.Error(fmt.Sprintf("Error writing bytes to buffer: %v", err))
						return
					}
				}

				f := getPodFileName(dumpDir, pod.ObjectMeta, container.Name)
				writeToFile(f, buffer.String())

				stream.Close()
			}
		}(p)
	}

	wg.Wait()
}
