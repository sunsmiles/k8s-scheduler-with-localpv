package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
	"log"
	"net/http"
)

//从标准的http.Request请求提中提取对应的pod和node等信息；并将处理之后的结果写入到response中
func LocalPVPredicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		io.WriteString(w, "404")
		return
	}

	var buf bytes.Buffer
	body := io.TeeReader(r.Body, &buf)
	log.Print("info: ", "localpv predicate", " ExtenderArgs = ", buf.String())

	var extenderArgs schedulerapi.ExtenderArgs
	var extenderFilterResult *schedulerapi.ExtenderFilterResult

	if err := json.NewDecoder(body).Decode(&extenderArgs); err != nil {
		extenderFilterResult = &schedulerapi.ExtenderFilterResult{
			Nodes:       nil,
			FailedNodes: nil,
			Error:       err.Error(),
		}
	} else {
		//实际的predicate处理过程
		extenderFilterResult = pridicateProcedure(extenderArgs)
	}

	if resultBody, err := json.Marshal(extenderFilterResult); err != nil {
		panic(err)
	} else {
		log.Print("info: ", "localpv predicate", " extenderFilterResult = ", string(resultBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(resultBody)
	}
}

/*
1. 如果pod存在localpv，则直接根据localpv找到对应的node，并返回；
2. 如果pod没有localpv，但node有localpv，则根据localpv找到所有对应的pod，
       1） 如果存在未调度的pod，则预留出相应的资源之后，判断node是否能够满足资源需求；
*/
func pridicateProcedure(args schedulerapi.ExtenderArgs) *schedulerapi.ExtenderFilterResult {
	canSchedule := make([]v1.Node, 0, len(args.Nodes.Items))
	canNotSchedule := make(map[string]string)

	pod := args.Pod
	nodes := args.Nodes

	//如果pod存在localpv
	if hasLocalPV := hasLocalPVOfPod(pod); hasLocalPV {
		nodeMap, _ := getLocalPVNodeFromPod(pod)
		if len(nodeMap) == 1 { //如果所有localpv都绑定到同一个node上面
			nodeName := ""
			for k := range nodeMap {
				nodeName = k
			}

			//如果找到对应的node，则将该node作为唯一候选节点放入canSchedule列表中
			node := getNode(nodeName)
			if node != nil {
				canSchedule = append(canSchedule, *node)
				//将其他节点放入cannotSchedule中
				for _, n := range nodes.Items {
					if n.Name != node.Name {
						canNotSchedule[n.Name] = "not the local pv node for pod"
					}
				}
			} else {
				//如果根据nodeName没有找到对应的node，则将所有node全部加入不可调度列表中
				for _, n := range nodes.Items {
					canNotSchedule[n.Name] = "not the local pv node for pod"
				}
			}
		} else if len(nodeMap) > 1 {
			//如果所有localpv绑定到了多个node上面，是不应该出现的不合理情况，将所有node全部加入不可调度列表中
			for _, n := range nodes.Items {
				canNotSchedule[n.Name] = "Not the local pv bind node."
			}
		}
	} else {
		//如果pod没有声明local pv，则检查是否node有未绑定的要求local pv的pod，有则扣除对应的资源
		for _, node := range nodes.Items {
			//检查扣除相应pod资源之后，是否还有空间存放当前pod，如果不能则加入到cannotSchedule列表
			if canHost(pod, &node) {
				canSchedule = append(canSchedule, node)
			} else {
				canNotSchedule[node.Name] = "Not enough resource after reserving for pod"
			}
		}
	}
	result := schedulerapi.ExtenderFilterResult{
		Nodes: &v1.NodeList{
			Items: canSchedule,
		},
		FailedNodes: canNotSchedule,
		Error:       "",
	}

	return &result
}

func canHost(pod *v1.Pod, node *v1.Node) bool {
	var totalCpu, totalMem int64
	totalCpu = 0
	totalMem = 0
	for _, container := range pod.Spec.Containers {
		cpu, _ := container.Resources.Requests.Cpu().AsInt64()
		totalCpu += cpu
		mem, _ := container.Resources.Requests.Memory().AsInt64()
		totalMem += mem
	}

	nodeCpu, _ := node.Status.Allocatable.Cpu().AsInt64()
	nodeMem, _ := node.Status.Allocatable.Memory().AsInt64()

	if hasLocalPVOfNode(node) {
		localPVPodsCpu, localPVPodsMem := getLocalPVPodResource(node)
		nodeCpu -= localPVPodsCpu
		nodeMem -= localPVPodsMem
	}

	return totalCpu < nodeCpu && totalMem < nodeMem
}

/*
根据node找到绑定的localpv，根据localpv找到关联的pods
为了将pods中没有调度到当前节点的pods对应的资源预留出来，需要计算这些pods需要多少资源。
*/
func getLocalPVPodResource(node *v1.Node) (cpu int64, mem int64) {
	//node.Status.Allocatable.Pods().AsInt64()
	//node.Status.VolumesAttached

	//找到所有的pv，过滤出所有的localpv，并且绑定的node节点为当前node
	pvs, err := k8scli.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	if err != nil {
		return 0, 0
	}

	//找到所有的localpv
	currentNodelocalPVs := make(map[string]*v1.PersistentVolume)
	for _, pv := range pvs.Items {
		if isLocalPV(&pv) {
			nodeName, err := getNodeNameFromPV(&pv)
			if err != nil {
				continue
			}
			if nodeName == node.Name {
				currentNodelocalPVs[pv.Name] = &pv
			}
		}
	}

	//找到所有未调度的pods
	pods, err := k8scli.CoreV1().Pods("default").List(metav1.ListOptions{})
	if err != nil {
		return 0, 0
	}
	unschedulePods := make(map[string]*v1.Pod)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" || pod.Status.Phase == "pending" {
			unschedulePods[pod.Name] = &pod
		}
	}

	var reservedCpu, reservedMem int64
	reservedCpu = 0
	reservedMem = 0
	for _, unpod := range unschedulePods {
		for _, v := range unpod.Spec.Volumes {
			if _, ok := currentNodelocalPVs[v.Name]; ok {
				for _, con := range unpod.Spec.Containers {
					rcpu, _ := con.Resources.Requests.Cpu().AsInt64()
					reservedCpu += rcpu
					rmem, _ := con.Resources.Requests.Memory().AsInt64()
					reservedMem += rmem
				}
			}
		}
	}

	return reservedCpu, reservedMem
}

func isLocalPV(pv *v1.PersistentVolume) bool {
	return pv.Spec.PersistentVolumeSource.Local != nil
}

func hasLocalPVOfNode(node *v1.Node) bool {
	for _, va := range node.Status.VolumesAttached {
		pv := getPV(string(va.Name))
		if pv.Spec.PersistentVolumeSource.Local != nil {
			return true
		}
	}
	return false
}

//检查pod是否存在localpv
func hasLocalPVOfPod(pod *v1.Pod) bool {
	//pod的local pv只存在于Volume挂载需求，如果没有Volume需要挂载则直接返回没有local pv
	vs := pod.Spec.Volumes
	if len(vs) <= 0 {
		return false
	}

	/*
	 * 如果有Volume，那么检查每个Volume，查看是否存在Volume为localpv
	 *     Volume是localpv的判断依据为：
	 *        1. 如果pod未绑定，name根据pvc.Spec.StorageClassName非空并且名称为"local-storage"，则认为需要localpv；
	 *       此处要求所有配置localpv的pvc.StorageClassName必须为"local-storage"
	 *        2. 根据Volume找到对应的pvcName，根据pvcName找到对应的pvc，根据pvc.Spec.PersistentVolumeSOurce.Local
	 *       是否非空判断是否为localpv
	 */

	for _, v := range vs {
		if v.PersistentVolumeClaim == nil {
			return false
		}
		pvcName := v.PersistentVolumeClaim.ClaimName
		pvc, err := k8scli.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(pvcName, metav1.GetOptions{})
		if err != nil {
			fmt.Println(err.Error())
			return false
		}

		//如果pvc未绑定到pv，那么根据storageClassName=="local-storage"来判断是否为localpv
		if pvc.Status.Phase != "bound" && pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName == "local-storage" {
			return true
		}

		if pvc.Spec.VolumeName == "" {
			fmt.Printf("Empty volume name of pvc with name: %s, status: %s, content of pvc: %v\n", pvcName, pvc.Status.Phase, pvc)
			continue
		}
		fmt.Println("Find volume by name " + pvc.Spec.VolumeName)

		pv, pvErr := k8scli.CoreV1().PersistentVolumes().Get(pvc.Spec.VolumeName, metav1.GetOptions{})
		if pvErr != nil {
			fmt.Println(pvErr.Error())
			return false
		}

		//如果pvc已经绑定到pv，则根据pv的source.Local来判断是否为localpv
		if pv.Spec.PersistentVolumeSource.Local != nil {
			return true
		}

	}
	return false
}

//找到含有localpv的pod对应的nodeName，nodeName放在map中是为了去重
func getLocalPVNodeFromPod(pod *v1.Pod) (localpvNodeMap map[string]string, err error) {
	volumes := pod.Spec.Volumes
	for _, v := range volumes {
		if v.PersistentVolumeClaim == nil {
			err = errors.New("empty pvc")
			return
		}

		pvcName := v.PersistentVolumeClaim.ClaimName
		pvc := getPVC(pvcName)
		pv := getPV(pvc.Spec.VolumeName)
		nodeName, err := getNodeNameFromPV(pv)
		if err != nil {
			fmt.Println(err.Error())
			continue
		}
		localpvNodeMap[nodeName] = ""
	}
	return
}

func getNodeNameFromPV(pv *v1.PersistentVolume) (nodeName string, err error) {
	nst := pv.Spec.NodeAffinity.Required.NodeSelectorTerms
	fmt.Printf("Pv name: %s, contents: %v\n", pv.Name, nst)
	for _, n := range nst {
		//fmt.Printf(" --> terms is %v\n", n.MatchExpressions[0].Values[0])
		matchExps := n.MatchExpressions
		if len(matchExps) <= 0 {
			err = errors.New("no match expressions")
			return
		}

		for _, exp := range matchExps {
			if len(exp.Key) <= 0 {
				err = errors.New("no key in match expressions")
				return
			}
			if exp.Key == "kubernetes.io/hostname" && len(exp.Values) > 0 {
				return exp.Values[0], nil
			}
		}
	}
	return "", errors.New("no NodeSelectorTerms found")
}

func getNode(nodeName string) *v1.Node {
	node, err := k8scli.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	return node
}

func getPV(pvName string) *v1.PersistentVolume {
	pv, err := k8scli.CoreV1().PersistentVolumes().Get(pvName, metav1.GetOptions{})
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	return pv
}

func getPVC(pvcName string) *v1.PersistentVolumeClaim {
	pvc, err := k8scli.CoreV1().PersistentVolumeClaims("default").Get(pvcName, metav1.GetOptions{})
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	return pvc
}
