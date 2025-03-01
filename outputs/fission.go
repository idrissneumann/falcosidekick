// SPDX-License-Identifier: Apache-2.0
/*
Copyright (C) 2023 The Falco Authors.

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

package outputs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/falcosecurity/falcosidekick/types"
)

// Some constant strings to use in request headers
const FissionEventIDKey = "event-id"
const FissionEventNamespaceKey = "event-namespace"
const FissionContentType = "application/json"

// NewFissionClient returns a new output.Client for accessing Kubernetes.
func NewFissionClient(config *types.Configuration, stats *types.Statistics, promStats *types.PromStatistics,
	statsdClient, dogstatsdClient *statsd.Client) (*Client, error) {
	if config.Fission.KubeConfig != "" {
		restConfig, err := clientcmd.BuildConfigFromFlags("", config.Fission.KubeConfig)
		if err != nil {
			return nil, err
		}
		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, err
		}
		return &Client{
			OutputType:       Fission,
			Config:           config,
			Stats:            stats,
			PromStats:        promStats,
			StatsdClient:     statsdClient,
			DogstatsdClient:  dogstatsdClient,
			KubernetesClient: clientset,
		}, nil
	}

	endpointUrl := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/fission-function/%s", config.Fission.RouterService, config.Fission.RouterNamespace, config.Fission.RouterPort, config.Fission.Function)
	initClientArgs := &types.InitClientArgs{
		Config:          config,
		Stats:           stats,
		DogstatsdClient: dogstatsdClient,
		PromStats:       promStats,
		StatsdClient:    statsdClient,
	}

	return NewClient(Fission, endpointUrl, config.Fission.MutualTLS, config.Fission.CheckCert, *initClientArgs)
}

// FissionCall .
func (c *Client) FissionCall(falcopayload types.FalcoPayload) {
	c.Stats.Fission.Add(Total, 1)

	if c.Config.Fission.KubeConfig != "" {
		str, _ := json.Marshal(falcopayload)
		req := c.KubernetesClient.CoreV1().RESTClient().Post().AbsPath("/api/v1/namespaces/" +
			c.Config.Fission.RouterNamespace + "/services/" + c.Config.Fission.RouterService +
			":" + strconv.Itoa(c.Config.Fission.RouterPort) + "/proxy/" + "/fission-function/" +
			c.Config.Fission.Function).Body(str)
		req.SetHeader(FissionEventIDKey, uuid.New().String())
		req.SetHeader(ContentTypeHeaderKey, FissionContentType)
		req.SetHeader(UserAgentHeaderKey, UserAgentHeaderValue)

		res := req.Do(context.TODO())
		rawbody, err := res.Raw()
		if err != nil {
			go c.CountMetric(Outputs, 1, []string{"output:Fission", "status:error"})
			c.Stats.Fission.Add(Error, 1)
			c.PromStats.Outputs.With(map[string]string{"destination": "Fission", "status": Error}).Inc()
			log.Printf("[ERROR] : %s - %v\n", Fission, err.Error())
			return
		}
		log.Printf("[INFO]  : %s - Function Response : %v\n", Fission, string(rawbody))
	} else {
		c.httpClientLock.Lock()
		defer c.httpClientLock.Unlock()
		c.AddHeader(FissionEventIDKey, uuid.New().String())
		c.ContentType = FissionContentType

		err := c.Post(falcopayload)
		if err != nil {
			go c.CountMetric(Outputs, 1, []string{"output:Fission", "status:error"})
			c.Stats.Fission.Add(Error, 1)
			c.PromStats.Outputs.With(map[string]string{"destination": "Fission", "status": Error}).Inc()
			log.Printf("[ERROR] : %s - %v\n", Fission, err.Error())
			return
		}
	}
	log.Printf("[INFO]  : %s - Call Function \"%v\" OK\n", Fission, c.Config.Fission.Function)
	go c.CountMetric(Outputs, 1, []string{"output:Fission", "status:ok"})
	c.Stats.Fission.Add(OK, 1)
	c.PromStats.Outputs.With(map[string]string{"destination": "Fission", "status": OK}).Inc()
}
