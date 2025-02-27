package utils

import (
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

//
// StringSet helper functions
//
type StringSet map[string]bool

func UnionStringSet(a, b StringSet) StringSet {
	result := StringSet{}
	for _, m := range []StringSet{a, b} {
		for k := range m {
			result[k] = true
		}
	}
	return result
}

func SubtractStringSet(a, b StringSet) StringSet {
	result := StringSet{}
	for k := range a {
		if !b[k] {
			result[k] = true
		}
	}
	return result
}

func IntersectionStringSet(a, b StringSet) StringSet {
	result := StringSet{}
	for k, v := range a {
		if v && b[k] {
			result[k] = true
		}
	}
	return result
}

//
// k8s Node helper functions
//
func GetNodeNameSet(nodes []*corev1.Node) StringSet {
	result := StringSet{}
	for _, node := range nodes {
		result[node.Name] = true
	}
	return result
}

func hasTaint(node *corev1.Node, taintKey, value string, effect corev1.TaintEffect) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && taint.Effect == effect {
			if taint.Value == value {
				return true
			}
		}
	}
	return false
}

func FilterNodesWithFn(nodes []*corev1.Node, fn func(*corev1.Node) bool) []*corev1.Node {
	result := []*corev1.Node{}
	for _, node := range nodes {
		if fn(node) {
			result = append(result, node)
		}
	}
	return result
}

func FilterNodesWithTaintKeyValueEffect(nodes []*corev1.Node, taintKey, value string, effect corev1.TaintEffect) []*corev1.Node {
	return FilterNodesWithFn(nodes, func(node *corev1.Node) bool {
		return hasTaint(node, taintKey, value, effect)
	})
}

//
// k8s Pod helper functions
//
func IsPodUnschedulable(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Reason == corev1.PodReasonUnschedulable &&
			condition.Type == corev1.PodScheduled &&
			condition.Status == corev1.ConditionFalse {
			return true
		}
	}
	return false
}

func GetPodNameSet(pods []*corev1.Pod) StringSet {
	names := StringSet{}
	for _, pod := range pods {
		names[pod.Name] = true
	}

	return names
}

func GetPodNodeNameSet(pods []*corev1.Pod) StringSet {
	names := StringSet{}
	for _, pod := range pods {
		names[pod.Spec.NodeName] = true
	}
	return names
}

func FilterPodsWithFn(pods []*corev1.Pod, fn func(*corev1.Pod) bool) []*corev1.Pod {
	result := []*corev1.Pod{}
	for _, pod := range pods {
		if fn(pod) {
			result = append(result, pod)
		}
	}
	return result
}

func FilterPodsWithNodeInNameSet(pods []*corev1.Pod, nameSet StringSet) []*corev1.Pod {
	return FilterPodsWithFn(pods, func(pod *corev1.Pod) bool {
		return nameSet[pod.Spec.NodeName]
	})
}

func FilterPodsWithAnnotationKey(pods []*corev1.Pod, key string) []*corev1.Pod {
	return FilterPodsWithFn(pods, func(pod *corev1.Pod) bool {
		annos := pod.ObjectMeta.Annotations
		if annos != nil {
			_, ok := annos[key]
			return ok
		}
		return false
	})
}

func FilterPodsWithLabel(pods []*corev1.Pod, label, value string) []*corev1.Pod {
	return FilterPodsWithFn(pods, func(pod *corev1.Pod) bool {
		labels := pod.Labels
		if labels != nil {
			labelValue, ok := labels[label]
			return ok && labelValue == value
		}
		return false
	})
}

//
// k8s PVC helpers
//
func FilterPVCsWithFn(pvcs []*corev1.PersistentVolumeClaim, fn func(*corev1.PersistentVolumeClaim) bool) []*corev1.PersistentVolumeClaim {
	result := []*corev1.PersistentVolumeClaim{}
	for _, pvc := range pvcs {
		if fn(pvc) {
			result = append(result, pvc)
		}
	}
	return result
}

func GetPVCSelectedNodeName(pvc *corev1.PersistentVolumeClaim) string {
	annos := pvc.Annotations
	if annos == nil {
		annos = map[string]string{}
	}
	pvcNode := annos["volume.kubernetes.io/selected-node"]
	return pvcNode
}

//
// Migrated from operator-sdk, these are internal in newer versions
//

// ForceRunModeEnv indicates if the operator should be forced to run in either local
// or cluster mode (currently only used for local mode)
var ForceRunModeEnv = "OSDK_FORCE_RUN_MODE"

type RunModeType string

const (
	LocalRunMode   RunModeType = "local"
	ClusterRunMode RunModeType = "cluster"
)

var log = logf.Log.WithName("k8sutil")

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which is the namespace where the watch activity happens.
	// this value is empty if the operator is running with clusterScope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"
)

// GetWatchNamespace returns the namespace the operator should be watching for changes
func GetWatchNamespace() (string, error) {
	ns, found := os.LookupEnv(WatchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", WatchNamespaceEnvVar)
	}
	return ns, nil
}

// ErrNoNamespace indicates that a namespace could not be found for the current
// environment
var ErrNoNamespace = fmt.Errorf("namespace not found for current environment")

// ErrRunLocal indicates that the operator is set to run in local mode (this error
// is returned by functions that only work on operators running in cluster mode)
var ErrRunLocal = fmt.Errorf("operator run mode forced to local")

// GetOperatorNamespace returns the namespace the operator should be running in.
func GetOperatorNamespace() (string, error) {
	if isRunModeLocal() {
		return "", ErrRunLocal
	}
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoNamespace
		}
		return "", err
	}
	ns := strings.TrimSpace(string(nsBytes))
	log.V(1).Info("Found namespace", "Namespace", ns)
	return ns, nil
}

func isRunModeLocal() bool {
	return os.Getenv(ForceRunModeEnv) == string(LocalRunMode)
}

// GetGVKsFromAddToScheme takes in the runtime scheme and filters out all generic apimachinery meta types.
// It returns just the GVK specific to this scheme.
func GetGVKsFromAddToScheme(addToSchemeFunc func(*runtime.Scheme) error) ([]schema.GroupVersionKind, error) {
	s := runtime.NewScheme()
	err := addToSchemeFunc(s)
	if err != nil {
		return nil, err
	}
	schemeAllKnownTypes := s.AllKnownTypes()
	ownGVKs := []schema.GroupVersionKind{}
	for gvk := range schemeAllKnownTypes {
		if !isKubeMetaKind(gvk.Kind) {
			ownGVKs = append(ownGVKs, gvk)
		}
	}

	return ownGVKs, nil
}

func isKubeMetaKind(kind string) bool {
	if strings.HasSuffix(kind, "List") ||
		kind == "PatchOptions" ||
		kind == "GetOptions" ||
		kind == "DeleteOptions" ||
		kind == "ExportOptions" ||
		kind == "APIVersions" ||
		kind == "APIGroupList" ||
		kind == "APIResourceList" ||
		kind == "UpdateOptions" ||
		kind == "CreateOptions" ||
		kind == "Status" ||
		kind == "WatchEvent" ||
		kind == "ListOptions" ||
		kind == "APIGroup" {
		return true
	}

	return false
}
