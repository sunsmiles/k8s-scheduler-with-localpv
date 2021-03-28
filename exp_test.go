package main

import (
	"encoding/json"
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

func init(){
	k8scli, _ = createKubernetesClient()
}
func TestK8scli(t*testing.T) {
	for {
		items, err := k8scli.CoreV1().Pods("").List(metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		fmt.Printf("The number of resource is : %d.\n", len(items.Items))

		pods, err := k8scli.CoreV1().Pods("").List(metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))

		// Examples for error handling:
		// - Use helper functions like e.g. errors.IsNotFound()
		// - And/or cast to StatusError and use its properties like e.g. ErrStatus.Message
		namespace := "default"
		podName := "web-0"
		var pod *v1.Pod
		pod, err = k8scli.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			fmt.Printf("Pod %s in namespace %s not found\n", podName, namespace)
		} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
			fmt.Printf("Error getting pod %s in namespace %s: %v\n",
				podName, namespace, statusError.ErrStatus.Message)
		} else if err != nil {
			panic(err.Error())
		} else {
			fmt.Printf("Found pod %s in namespace %s\n", podName, namespace)
			volumes := pod.Spec.Volumes
			if len(volumes) > 0 {
				fmt.Printf("There are %d volumes of current pod.\n", len(volumes))
				for i, vv := range volumes {
					if vv.HostPath != nil {
						fmt.Printf("The content of volume %d with name %s is :%v\n", i, vv.Name, vv.HostPath.Path)
					}

					if vv.PersistentVolumeClaim != nil {
						fmt.Printf("Name of PersistentVolumeClaim is %s.\n", vv.PersistentVolumeClaim.ClaimName)
					} else {
						fmt.Println("Empty pointer of PersistentVolumeClaim.")
					}

				}
			} else {
				fmt.Println("There are no volume of current pod.")
			}

		}

		time.Sleep(10 * time.Second)
	}

}

func TestPod(t*testing.T) {
	pods, err := k8scli.CoreV1().Pods("default").List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("Number of pods listed is: %d\n", len(pods.Items))

	var ppod *v1.Pod
	for i, p := range pods.Items {
		ppod = &p
		fmt.Printf("The contents of pod %d are: %v\n", i, ppod.Name)
		if hasLocalPVOfPod(ppod) {
			fmt.Println(" --> Has local pv")
		} else {
			fmt.Println(" --> No local pv")
		}
	}
}

func TestPVC(t*testing.T) {
	pvc, err := k8scli.CoreV1().PersistentVolumeClaims("default").Get("k8s-local-claim-web-0", metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("pvc name: %s, status: %v, storageClassName:%s, contents:%v\n", pvc.Name, pvc.Status.Phase, *pvc.Spec.StorageClassName, pvc)
}

func TestPV(t*testing.T) {
	pvs, err := k8scli.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("Number of pvs listed is: %d\n", len(pvs.Items))
	for i, pv := range pvs.Items {
		nst := pv.Spec.NodeAffinity.Required.NodeSelectorTerms

		nodeName, errs := getNodeNameFromPV(&pv)
		if errs != nil {
			fmt.Printf("Pv %d name: %s, related node: %s, contents: %v\n", i, pv.Name, "no node", nst)
		} else {
			fmt.Printf("Pv %d name: %s, related node: %s, contents: %v\n", i, pv.Name, nodeName, nst)
		}

		for j, n := range nst {
			fmt.Printf(" --> terms %d is %v, volume source : %v\n", j, n.MatchExpressions[0].Values[0], pv.Spec.PersistentVolumeSource.Local)
		}
	}
}

func TestNode(t*testing.T) {
	nodes, err := k8scli.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		t.Error(err.Error())
		return
	}
	t.Logf("Number of nodes listed is: %d\n", len(nodes.Items))
	for _, n := range nodes.Items {
		bytes, _ := json.Marshal(n)
		t.Logf("NodeName: %s, VolumeAttacthed:%v, VolumeInuse: %v, node: %v\n", n.Name, n.Status.VolumesAttached, n.Status.VolumesInUse, string(bytes))
	}
}

func TestHelloWorld(t*testing.T){
	t.Logf("Hello, World!")
}