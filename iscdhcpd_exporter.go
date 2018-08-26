// Copyright 2018 Andrea Spagnolo
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"
)

const (
	namespace = "iscdhcpd"
)

type Exporter struct {
	summaryFree    *prometheus.Desc
	summaryTouched *prometheus.Desc
	summaryUsed    *prometheus.Desc
	summaryDefined *prometheus.Desc
	scrapeFailures prometheus.Counter
}

type Subnet struct {
	Location string  `json:"location"`
	Range    string  `json:"range"`
	Defined  float64 `json:"defined"`
	Used     float64 `json:"used"`
	Touched  float64 `json:"touched"`
	Free     float64 `json:"free"`
}

type Lease struct {
	Subnets        []Subnet `json:"subnets"`
	SharedNetworks []string `json:"shared-networks"`
	Summary        Subnet   `json:"summary"`
}

func NewExporter() *Exporter {
	return &Exporter{
		summaryFree: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "summary", "free_total"),
			"IPs Free",
			nil,
			nil),
		summaryTouched: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "summary", "touched_total"),
			"IPs Touched",
			nil,
			nil),
		summaryUsed: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "summary", "used_total"),
			"IPs Used",
			nil,
			nil),
		summaryDefined: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "summary", "defined_total"),
			"IPs Defined",
			nil,
			nil),
		scrapeFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_scrape_failures_total",
			Help:      "Number of errors while scraping apache.",
		}),
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.summaryFree
	ch <- e.summaryTouched
	ch <- e.summaryUsed
	ch <- e.summaryDefined
	e.scrapeFailures.Describe(ch)
}

func getoutputPool() Lease {
	var stdBuffer bytes.Buffer
	outputPools := exec.Command("/usr/bin/dhcpd-pools", "-c", "/etc/dhcp/dhcpd.conf", "--leases=/var/lib/dhcp/dhcpd.leases", "-f", "j")
	mw := io.MultiWriter(os.Stdout, &stdBuffer)
	outputPools.Stdout = mw
	err := outputPools.Run()
	if err != nil {
		log.Errorf("Error: %s", err)
	}

	var lease Lease
	err = json.Unmarshal(stdBuffer.Bytes(), &lease)

	if err != nil {
		log.Errorf("Error: %s", err)
	}

	return lease
}

func (e *Exporter) collect(ch chan<- prometheus.Metric) error {
	outputPool := getoutputPool()
	ch <- prometheus.MustNewConstMetric(e.summaryFree, prometheus.CounterValue, outputPool.Summary.Free)
	ch <- prometheus.MustNewConstMetric(e.summaryTouched, prometheus.CounterValue, outputPool.Summary.Touched)
	ch <- prometheus.MustNewConstMetric(e.summaryUsed, prometheus.CounterValue, outputPool.Summary.Used)
	ch <- prometheus.MustNewConstMetric(e.summaryDefined, prometheus.CounterValue, outputPool.Summary.Defined)
	return nil
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	if err := e.collect(ch); err != nil {
		log.Errorf("Error scraping dhcpd-pools: %s", err)
		e.scrapeFailures.Inc()
		e.scrapeFailures.Collect(ch)
	}
	return
}

func main() {
	var (
		listenAddress = kingpin.Flag("web.listen-address", "Address on which to expose metrics and web interface.").Default(":9367").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	)

	log.AddFlags(kingpin.CommandLine)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	exporter := NewExporter()
	prometheus.MustRegister(exporter)
	prometheus.MustRegister(version.NewCollector("iscdhcpd_exporter"))

	log.Infoln("Starting iscdhcpd_exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
                        <head><title>Node Exporter</title></head>
                        <body>
                        <h1>ISC-DHCPD Exporter</h1>
                        <p><a href="` + *metricsPath + `">Metrics</a></p>
                        </body>
                        </html>`))
	})
	log.Infoln("Listening on", *listenAddress)
	err := http.ListenAndServe(*listenAddress, nil)
	if err != nil {
		log.Fatal(err)
	}
}
