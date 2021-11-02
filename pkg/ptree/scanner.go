package ptree

import (
	"bufio"
	"io/ioutil"
	"k8s.io/klog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
)
const(
	QOSGuaranteed = "guaranteed"
	QOSBurstable  = "burstable"
	QOSBestEffort = "besteffort"
	CgroupBase    = "/host/sys/fs/cgroup/memory"
	PodPrefix     = "pod"
	CgroupProcs   = "cgroup.procs"
	kubeRoot      = "kubepods"
)

var (
	validShortID = regexp.MustCompile("^[a-f0-9]{64}$")
)

func IsContainerID(id string) bool {
	return validShortID.MatchString(id)
}

type Scanner interface {
	Scan(UID, QOS string) (Pod, error, bool)
	deletePod(UID string)
	deleteContainer(UID string)
}

type ScannerImpl struct{
	nodeCache *Node
	mu        sync.Mutex
}

func NewScanner() Scanner {
	return &ScannerImpl{
		nodeCache:    NewNode(),
		mu:           sync.Mutex{},
	}
}

func (scan *ScannerImpl) deletePod(UID string){
	scan.mu.Lock()
	defer scan.mu.Unlock()
	delete(scan.nodeCache.Pods, UID)
}

func (scan *ScannerImpl) deleteContainer(UID string){
	scan.mu.Lock()
	defer scan.mu.Unlock()
	for _, container := range scan.nodeCache.Containers {
		if container.Parent == UID {
			delete(scan.nodeCache.Containers,container.ID)
		}
	}
}

type CgroupName []string

func (scan *ScannerImpl) Scan(UID, QOS string) (Pod, error, bool) {
	pod, err, exist := scan.getContainers(NewPod(QOS, UID))
	if !exist{
		return Pod{}, nil, exist
	}
	if err != nil {
		klog.Errorf("Cannot scan pod: pod%s, %v", UID, err)
		return Pod{}, err, true
	}
	return *pod, nil, true
}

func (scan *ScannerImpl) getContainers(p *Pod) (*Pod, error, bool) {
	podPath := scan.getPodPath(p.UID, p.QOS)
	basePodPath := filepath.Clean(filepath.Join(CgroupBase, podPath))
	containers, err, exist := scan.readContainerFile(basePodPath, p, p.UID)
	if !exist {
		return nil, nil, exist
	}
	if err !=nil {
		klog.Errorf("Cannot read the containers in the pod: pod%s, %v", p.UID, err)
		return nil, err, true
	}
	return &Pod{
		UID:        p.UID,
		QOS:        p.QOS,
		Containers: containers,
	},nil, true
}

//getPodPath is to get the path of the pod ,such as:kubepods/besteffort/pod17eb80b0-6085-4d12-8e79-553e799d2f0b
func (scan *ScannerImpl) getPodPath(UID string, QOS string) (podPath string) {
	var parentPath CgroupName
	switch QOS {
	case QOSGuaranteed:
		parentPath = append(parentPath,kubeRoot)
	case QOSBurstable:
		parentPath = append(parentPath, kubeRoot, QOSBurstable)
	case QOSBestEffort:
		parentPath = append(parentPath, kubeRoot, QOSBestEffort)
	}
	podContainer := PodPrefix + UID
	parentPath = append(parentPath,podContainer)
	podPath = scan.transformToPath(parentPath)
	return podPath
}

func (scan *ScannerImpl) transformToPath(cgroupName CgroupName) string {
	return "/" + path.Join(cgroupName...)
}

func (scan *ScannerImpl)readContainerFile(podPath string, pod *Pod, UID string) (map[string]*Container, error, bool) {
	if !IsExist(podPath) {
		return nil, nil, false
	}
	fileList, err := ioutil.ReadDir(podPath)
	if err != nil {
		klog.Errorf("Can't read %s, %v", podPath, err)
		return nil, err, true
	}
	for _,file :=range fileList {
		containerId := file.Name()
		if IsContainerID(containerId) {
			if scan.nodeCache.Pods[UID] == nil {
				scan.nodeCache.Pods[UID] = NewPod(pod.QOS, pod.UID)
			}
			scan.nodeCache.Pods[UID].AddContainer(containerId)
			scan.nodeCache.Pods[UID].Containers[containerId] =  &Container{
				ID: containerId,
				Parent: pod.UID,
			}
			procPath := filepath.Join(podPath, containerId, CgroupProcs)
			process, err := scan.readPidFile(procPath, scan.nodeCache.Pods[UID].Containers[containerId], containerId)
			if err != nil {
				klog.Errorf("Cannot read the pid in the container: %s, %v", containerId, err)
				return nil, err, true
			}
			scan.nodeCache.Pods[UID].Containers[containerId].Processes = process
			}
	}
	return scan.nodeCache.Pods[UID].Containers, nil, true
}

func (scan *ScannerImpl)readPidFile(procPath string, container *Container, containerId string) (map[int]*Process, error) {
	file, err := os.Open(procPath)
	if err != nil {
		klog.Errorf("Cannot read %s, %v", procPath, err)
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if scan.nodeCache.Containers[containerId] == nil {
			scan.nodeCache.Containers[containerId] = NewContainer(containerId)

		}
		if pid, err := strconv.Atoi(line); err == nil {
			scan.nodeCache.Containers[containerId].AddProcess(pid)
			scan.nodeCache.Containers[containerId].Processes[pid] =  &Process{
				Pid: pid,
				Parent: container,
			}
		}
	}
	klog.V(4).Infof("Read from %s, pids", procPath, scan.nodeCache.Containers[containerId].Processes)
	return scan.nodeCache.Containers[containerId].Processes, nil
}

func IsExist(path string) bool {
	_, err := os.Stat(path)
	return err == nil || os.IsExist(err)
}