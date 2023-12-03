// TODO: Change Copyright to your company if open sourcing or remove header
//
// Copyright (c) 2021 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package config

// This file contains example of custom configuration that can be loaded from the service's configuration.toml
// and/or the Configuration Provider, aka Consul (if enabled).
// For more details see https://docs.edgexfoundry.org/2.2/microservices/application/GeneralAppServiceConfig/#custom-configuration
// TODO: Update this configuration as needed for you service's needs and remove this comment
//       or remove this file if not using custom configuration.

import (
	"errors"
	"reflect"
)

type StudentEyeKmFeature struct {
	StudentID             string `json:"student_id"`
	TaskID                *int   `json:"task_id,omitempty"`
	EyeKmCollectTimestamp string `json:"eye_km_collect_timestamp"`
	EyeTrackingData       []byte `json:"eye_tracking_data"`
	KeyboadrMouseData     []byte `json:"keyboadr_mouse_data"`
	EyeKmModelResult      string `json:"eye_km_model_result"`
}

type StudentFacialEEGFeature struct {
	StudentID                 string `json:"student_id"`
	TaskID                    *int   `json:"task_id,omitempty"`
	FacialEegCollectTimestamp string `json:"facial_eeg_collect_timestamp"`
	FacialExpression          []byte `json:"facial_expression"`
	EegData                   []byte `json:"eeg_data"`
	FacialEegModelResult      string `json:"facial_eeg_model_result"`
}

type ServiceConfig struct {
	APPService APPServiceConfig
}

type APPServiceConfig struct {
	RPCServerInfo        RemoteServerInfo
	EncryptionServerInfo RemoteServerInfo
	CloudServerInfo      RemoteServerInfo
}

type RemoteServerInfo struct {
	FacialAndEEGUrl string
	EyeAndKmUrl     string
	Timeout         int
	Protocol        string
}

// TODO: Update using your Custom configuration type.
// UpdateFromRaw updates the service's full configuration from raw data received from
// the Service Provider.
func (c *ServiceConfig) UpdateFromRaw(rawConfig interface{}) bool {
	configuration, ok := rawConfig.(*ServiceConfig)
	if !ok {
		return false //errors.New("unable to cast raw config to type 'ServiceConfig'")
	}

	*c = *configuration

	return true
}

// Validate ensures your custom configuration has proper values.
// TODO: Update to properly validate your custom configuration
func (asc *APPServiceConfig) Validate() error {
	if reflect.DeepEqual(asc.RPCServerInfo, RemoteServerInfo{}) {
		return errors.New("RPCServerInfo is not set")
	}
	if reflect.DeepEqual(asc.EncryptionServerInfo, RemoteServerInfo{}) {
		return errors.New("EncryptionInfo is not set")
	}
	if reflect.DeepEqual(asc.CloudServerInfo, RemoteServerInfo{}) {
		return errors.New("CloudServerInfo is not set")
	}

	return nil
}
