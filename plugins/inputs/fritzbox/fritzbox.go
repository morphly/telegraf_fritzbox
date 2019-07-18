package fritzbox

// fritzbox.go

// Copyright 2019 Stefan Förster, original code in main.go taken from
// https://github.com/ndecker/fritzbox_exporter, original copyright
// notice:
// Copyright 2016 Nils Decker
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"fmt"
	"log"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	upnp "github.com/morphly/fritzbox_exporter/fritzbox_upnp"
)

const serviceLoadRetryTime = 1 * time.Minute

type Fritzbox struct {
	Host     string
	Username string
	Password string
	Port     uint16
}

type Metric struct {
	Service string
	Action  string
	Result  string
	Name    string
}

type SubResult struct {
	Tags    map[string]string
	Results map[string]string
}

//ComplexMetric struct
type ComplexMetric struct {
	Service      string
	ServiceCount int
	Action       string
	Result       string
	Name         string
	SubAction    string
	SubResults   SubResult
}

var metrics = []*Metric{
	{
		Service: "urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1",
		Action:  "GetTotalPacketsReceived",
		Result:  "TotalPacketsReceived",
		Name:    "packets_received",
	},
	{
		Service: "urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1",
		Action:  "GetTotalPacketsSent",
		Result:  "TotalPacketsSent",
		Name:    "packets_sent",
	},
	{
		Service: "urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1",
		Action:  "GetAddonInfos",
		Result:  "TotalBytesReceived",
		Name:    "bytes_received",
	},
	{
		Service: "urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1",
		Action:  "GetAddonInfos",
		Result:  "TotalBytesSent",
		Name:    "bytes_sent",
	},
	{
		Service: "urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1",
		Action:  "GetCommonLinkProperties",
		Result:  "PhysicalLinkStatus",
		Name:    "link_status",
	},
	{
		Service: "urn:schemas-upnp-org:service:WANIPConnection:1",
		Action:  "GetStatusInfo",
		Result:  "ConnectionStatus",
		Name:    "connection_status",
	},
	{
		Service: "urn:schemas-upnp-org:service:WANIPConnection:1",
		Action:  "GetStatusInfo",
		Result:  "Uptime",
		Name:    "uptime",
	},
}

var complexMetrics = []*ComplexMetric{
	{
		Service:      "urn:dslforum-org:service:WLANConfiguration",
		ServiceCount: 3,
		Action:       "GetTotalAssociations",
		Result:       "TotalAssociations",
		SubAction:    "GetGenericAssociatedDeviceInfo",
		Name:         "fritzbox-wifi",
		SubResults: SubResult{
			Tags:    map[string]string{"wlan_device_mac": "AssociatedDeviceMACAddress", "wlan_device_ip": "AssociatedDeviceIPAddress"},
			Results: map[string]string{"wlan_device_signal": "X_AVM-DE_SignalStrength", "wlan_device_speed": "X_AVM-DE_Speed"},
		},
	},
}

func (s *Fritzbox) Description() string {
	return "a demo plugin"
}

func (s *Fritzbox) SampleConfig() string {
	return `
  ## Host and Port for FRITZ!Box UPnP service
  host = fritz.box
  port = 49000
`
}

func (s *Fritzbox) Gather(acc telegraf.Accumulator) error {

	var host string
	var username string
	var password string
	var port uint16
	if s.Host == "" {
		host = "fritz.box"
	} else {
		host = s.Host
	}
	if s.Port == 0 {
		port = 49000
	} else {
		port = s.Port
	}

	password = s.Password

	username = s.Username
	var err error

	root, err := upnp.LoadServices(host, port, username, password)
	if err != nil {
		return fmt.Errorf("fritzbox: unable to load services: %v", err)
	}

	//for s := range root.Services {
	//	log.Println(s)
	//log.Println(root.Services[s])
	//}

	//log.Println(len(root.Services))
	// remember what we already called
	var last_service string
	var last_method string
	var result upnp.Result
	fields := make(map[string]interface{})

	for _, m := range metrics {
		if m.Service != last_service || m.Action != last_method {
			service, ok := root.Services[m.Service]
			if !ok {
				// TODO
				log.Println("W! Cannot find defined service %s", m.Service)
				continue
			}
			action, ok := service.Actions[m.Action]
			if !ok {
				// TODO
				log.Println("W! Cannot find defined action %s on service %s", m.Action)
				continue
			}

			result, err = action.Call()
			if err != nil {
				log.Println("E! Unable to call action %s on service %s: %v", m.Action, m.Service, err)
				continue
			}

			// save service and action
			last_service = m.Service
			last_method = m.Action
		}

		fields[m.Name] = result[m.Result]
	}
	acc.AddFields("fritzbox", fields, map[string]string{"fritzbox": host})

	for _, m := range complexMetrics {

		for s := 1; s <= m.ServiceCount; s++ {
			if m.Service != last_service || m.Action != last_method {
				servicename := fmt.Sprintf("%s:%v", m.Service, s)
				service, ok := root.Services[servicename]

				if !ok {
					log.Println("W! Cannot find defined service %s", servicename)
					//log.Println(root.Services)
					continue
				}
				action, ok := service.Actions[m.Action]
				if !ok {
					// TODO
					log.Println("W! Cannot find defined action %s on service %s", m.Action)
					continue
				}

				result, err = action.Call()
				if err != nil {
					log.Println("E! Unable to call action %s on service %s: %v", m.Action, servicename, err)
					continue

				}

				val, ok := result[m.Result]
				if !ok {
					log.Println("result not found", m.Result)
					continue
				}

				var floatval int
				switch tval := val.(type) {
				case uint64:
					floatval = int(tval)
				default:
					log.Println("unknown", val)

				}
				for i := 0; i < floatval; i++ {

					subaction, ok := service.Actions[m.SubAction]
					if !ok {
						// TODO
						log.Println("cannot find subaction", m.SubAction)

					}

					result, err = subaction.CallParam("NewAssociatedDeviceIndex", i)
					if err != nil {
						log.Println(err)

					}

					complexfields := make(map[string]interface{})
					complextags := make(map[string]string)
					complextags["service"] = fmt.Sprint(s)
					complextags["fritzbox"] = host

					for name, value := range m.SubResults.Tags {
						complextags[name] = fmt.Sprint(result[value])
					}

					for name, value := range m.SubResults.Results {
						complexfields[name] = result[value]
					}

					acc.AddFields(m.Name, complexfields, complextags)

				}

			}
		}
		// save service and action
		last_service = m.Service
		last_method = m.Action

	}

	return nil
}

func init() {
	inputs.Add("fritzbox", func() telegraf.Input { return &Fritzbox{} })
}
