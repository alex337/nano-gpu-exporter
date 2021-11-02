package main

import (
	"flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"nano-gpu-exporter/pkg/exporter"
	"nano-gpu-exporter/pkg/util"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const Resources = "nvidia.com/gpu, tke.cloud.tencent.com/qgpu-core, tke.cloud.tencent.com/qgpu-memory, nano-gpu/gpu-percent"

var (
	node      string
	resources string
	interval  int
)

func init(){
	flag.StringVar(&node, "node", "", "node name")
	flag.StringVar(&resources, "labels", Resources, "gpu resources name")
	flag.IntVar(&interval, "interval", 30, "monitor interval (second)")
	flag.Parse()
}

func main() {
	e := exporter.NewExporter(node, strings.Split(resources, ","), time.Duration(interval) * time.Second)
	go e.Run(util.NeverStop)

	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			DisableCompression: true,
		},
	))
	port := os.Getenv("PORT")
	if _, err := strconv.Atoi(port); err != nil {
		port = "9500"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}