package exporter

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"nano-gpu-exporter/pkg/kubepods"
	"nano-gpu-exporter/pkg/metrics"
	"nano-gpu-exporter/pkg/nvidia"
	tree "nano-gpu-exporter/pkg/ptree"
	"nano-gpu-exporter/pkg/util"
	"strconv"
	"time"
	//"github.com/alex337/go-nvml"
	"tkestack.io/nvml"
)

const (
	HundredCore = 100
	GiBToMiB    = 1024
)
type Exporter struct {
	node       string
	gpuLabels  []string
	interval   time.Duration
	podCache   Cache
	contCache  ContainerCache
	ptree      tree.PTree
	collector  *metrics.Collector
    device     *nvidia.DeviceImpl
	watcher    kubepods.Watcher
}

func NewExporter(node string, gpuLabels []string, interval time.Duration) *Exporter {
	collector := metrics.NewCollector()
	collector.Register()
	ptree := tree.NewPTree(interval)
	podCache := NewCache()
	contCache := NewContCache()
	return &Exporter{
		node:      node,
		gpuLabels: gpuLabels,
		interval:  interval,
		podCache:  podCache,
		contCache: contCache,
		ptree:     ptree,
		collector: collector,

		watcher: kubepods.NewWatcher(&kubepods.Handler{
			AddFunc: func(pod *v1.Pod) {
				podCache.AddPod(string(pod.UID), pod)
				ptree.InterestPod(string(pod.UID), util.QoS(pod))
			},
			DelFunc: func(pod *v1.Pod) {
				collector.DeletePod(node, pod.Namespace, string(pod.UID))
				containerMap, _ := contCache.GetContainer(string(pod.UID))
                for _, name := range containerMap {
					collector.DeleteContainer(node, pod.Namespace, string(pod.UID), name)
				}
				podCache.DelPod(string(pod.UID))
				contCache.DelContainer(string(pod.UID))
				ptree.ForgetPod(string(pod.UID))
				ptree.DeleteScanner(string(pod.UID))
			},
			UpdateFunc: func(oldPod *v1.Pod, newPod *v1.Pod) {
				needUpdate := false
				if podCache.KnownPod(string(oldPod.UID)) && util.IsCompletePod(newPod) {
					needUpdate = true
				}
				//if !podCache.KnownPod(string(oldPod.UID)) && util.IsAssumed(newPod) {
				//	needUpdate = true
				//}
				if containerMap, _ := contCache.GetContainer(string(oldPod.UID));containerMap == nil{
					needUpdate = true
				}
				if needUpdate {
					podCache.AddPod(string(oldPod.UID), newPod)
				}
				return
			},

		}, gpuLabels, node),
	}
}

func (e *Exporter) Once() {
	nvml.Init()
	defer nvml.Shutdown()
	cardCount, err := nvml.DeviceGetCount()

	klog.Info("Exporter run")
	if err != nil{
		klog.Error("Cannot get DeviceGetCount by nvml")
	}
	cardUsages := make([]tree.CardUsage, cardCount)
	processUsages := make([]map[int]*tree.ProcessUsage, cardCount)
	for i := 0; i < int(cardCount); i++ {
		processUsages[i], err = e.device.GetDeviceUsage(i)
		if err != nil{
			klog.Errorf("Cannot get processusage in GPU %d", i)
		}
	}
	var totalMem, GPUMem uint64
	for i := 0; i < int(cardCount); i++ {
		dev, err := nvml.DeviceGetHandleByIndex(uint(i))
		if err != nil{
			klog.Error("DeviceGetHandleByIndex", err)
		}
		_, _, memTotal, err := dev.DeviceGetMemoryInfo()
		totalMem += memTotal >> 20
		GPUMem = memTotal >> 20
	}
	node := e.ptree.Snapshot()
	for _, pod := range node.Pods{
		p, _ := e.podCache.GetPod(pod.UID)
		if containerMap, exist := e.contCache.GetContainer(pod.UID); !exist || containerMap == nil{
			e.contCache.AddContainer(p)
		}
		ns := p.Namespace
		var podCore, podMem, podCoreRequest, podMemRequest float64
		for _, container := range pod.Containers{
			contName, exist := e.contCache.GetContainerName(pod.UID, fmt.Sprintf(util.ContainerID, container.ID))
			if !exist {
				continue
			}
			var contCore, contMem float64
			for _, proc := range container.Processes{
				for i := 0; i < int(cardCount); i++ {
					procUsage, exist := processUsages[i][proc.Pid]
					if exist {
						contMem  += procUsage.GPUMem
						contCore += procUsage.GPUCore
						klog.Info("contCore:",contCore)
						cardUsages[i].Mem  += procUsage.GPUMem
						cardUsages[i].Core += procUsage.GPUCore
					}
				}
			}
			podCore += contCore
			podMem += contMem
			var memRequest, coreRequest float64
			for _,cont := range p.Spec.Containers{
				if contName == cont.Name {
					if util.GetPercentFromContainer(&cont) != 0 {
						coreRequest = float64(util.GetPercentFromContainer(&cont))
						memRequest = float64(GPUMem) * coreRequest / HundredCore
					} else {
						memRequest = float64(util.GetGPUMemoryFromContainer(&cont)) * GiBToMiB
						coreRequest = float64(util.GetGPUCoreFromContainer(&cont))
					}
				}
			}
			podCoreRequest += coreRequest
			podMemRequest += memRequest

			var contCoreUtil float64
			var contMemUtil float64
			if contCore != 0 && coreRequest != 0{
				contCoreUtil = contCore / coreRequest
			}
			if contMem != 0 && memRequest != 0{
				contMemUtil = contMem / memRequest
			}
			e.collector.Container(e.node, ns, pod.UID, contName, contCore, contMem, util.Decimal(contCoreUtil * 100), util.Decimal(contMemUtil * 100))
		}
		//podMem, podCore, podMemRequest, podCoreRequest := e.displayContUtil(pod, p, ns, cardCount, processUsages, cardUsages, GPUMem)

		var podMemUtil, podCoreUtil float64
		if podMemRequest != 0 && podMem != 0 {
			podMemUtil = podMem / podMemRequest
		}
		if podCoreRequest != 0 && podCore != 0 {
			podCoreUtil = podCore / podCoreRequest
		}

		e.collector.Pod(e.node, ns, pod.UID, podCore, podMem, util.Decimal(podCoreUtil * 100), util.Decimal(podMemUtil * 100), podMemRequest, util.Decimal(podCore / float64(cardCount * HundredCore) * 100), util.Decimal(podMem / float64(totalMem) * 100))
	}
	e.displayGPUUtil(cardCount, cardUsages)
}

func (e *Exporter) displayGPUUtil(cardCount uint, cardUsages []tree.CardUsage){
	for i := 0; i < int(cardCount); i++ {
		dev, err := nvml.DeviceGetHandleByIndex(uint(i))
		_, memUsed, memTotal, err := dev.DeviceGetMemoryInfo()
		//util1, _ := dev.DeviceGetAverageGPUUsage(time.Second)
		utilization, err := dev.DeviceGetUtilizationRates()
		if err != nil {
			klog.Error("DeviceGetMemoryInfo", err)
		}
		klog.Info("cardUsagesMem:", cardUsages[i].Mem)
		klog.Info("cardUsagesCore:", cardUsages[i].Core)

		if cardUsages[i].Mem >= 0 || cardUsages[i].Core >= 0 {
			e.collector.Card(e.node, strconv.Itoa(i), cardUsages[i].Core, float64(memUsed >> 20), util.Decimal(float64(utilization.GPU)), util.Decimal(float64(memUsed >> 20) / float64(memTotal >> 20) * 100))
		}
	}
}

func (e *Exporter) Run(stop <-chan struct{}) {
	go e.ptree.Run(stop)
	e.watcher.Run(stop)
	util.Loop(e.Once, e.interval, stop)
}