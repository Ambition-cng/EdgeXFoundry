// -*- Mode: Go; indent-tabs-mode: t -*-
//
// Copyright (C) 2017-2018 Canonical Ltd
// Copyright (C) 2018-2019 IOTech Ltd
//
// SPDX-License-Identifier: Apache-2.0

// This package provides a simple example of a device service.
package main

import (
	// "github.com/edgexfoundry/device-sdk-go/v2"
	// "github.com/edgexfoundry/device-sdk-go/v2/example/driver"

	"github.com/edgexfoundry/device-cccccf"
	"github.com/edgexfoundry/device-cccccf/driver"
	"github.com/edgexfoundry/device-sdk-go/v2/pkg/startup"
)

const (
	serviceName string = "device-socket"
)

func main() {
	sd := driver.SocketDriver{}
	startup.Bootstrap(serviceName, device.Version, &sd)
}
