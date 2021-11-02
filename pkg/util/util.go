package util

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"strconv"
	"time"
)

var NeverStop = make(chan struct{})

const AnnotationQGPUAssume    = "tke.cloud.tencent.com/qgpu"
// TODO: add recover
func Loop(f func(), duration time.Duration, stop <-chan struct{}) {
	for range time.Tick(duration) {
		select {
		case <- stop:
			return
		default:
			f()
		}
	}
}

func GetGPUCoreFromContainer(container *v1.Container) int {
	val, ok := container.Resources.Limits[ResourceGPUCore]
	if !ok {
		return 0
	}
	return int(val.Value())
}

func GetGPUMemoryFromContainer(container *v1.Container) int {
	val, ok := container.Resources.Limits[ResourceGPUMemory]
	if !ok {
		return 0
	}
	return int(val.Value())
}

func GetPercentFromContainer(container *v1.Container) int {
	val, ok := container.Resources.Limits[ResourceGPUPercent]
	if !ok {
		return 0
	}
	return int(val.Value())
}

func Decimal(value float64) float64 {
	value, _ = strconv.ParseFloat(fmt.Sprintf("%.2f", value), 64)
	return value
}

func IsCompletePod(pod *v1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return true
	}

	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return true
	}
	return false
}

func IsAssumed(pod *v1.Pod) bool {
	return pod.ObjectMeta.Annotations[AnnotationQGPUAssume] == "true"
}