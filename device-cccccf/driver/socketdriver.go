package driver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/edgexfoundry/device-cccccf/config"
	"github.com/edgexfoundry/device-sdk-go/v2/pkg/interfaces"
	sdkModels "github.com/edgexfoundry/device-sdk-go/v2/pkg/models"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/models"

	gometrics "github.com/rcrowley/go-metrics"
)

const readCommandsExecutedName = "ReadCommandsExecuted"

type DeviceInfo struct {
	deviceIP string
	conn     net.Conn
}

type SocketDriver struct {
	lc                   logger.LoggingClient
	asyncCh              chan<- *sdkModels.AsyncValues
	deviceCh             chan<- []sdkModels.DiscoveredDevice
	result               map[string]interface{}
	deviceInfo           map[string]DeviceInfo
	readCommandsExecuted gometrics.Counter
	serviceConfig        *config.ServiceConfig
	EEGAndFacial         string
	eegandfaciallistener net.Listener
}

func (s *SocketDriver) Initialize(lc logger.LoggingClient, asyncCh chan<- *sdkModels.AsyncValues, deviceCh chan<- []sdkModels.DiscoveredDevice) error {
	s.lc = lc
	s.asyncCh = asyncCh
	s.deviceCh = deviceCh
	s.serviceConfig = &config.ServiceConfig{}

	s.deviceInfo = make(map[string]DeviceInfo)

	ds := interfaces.Service()

	if err := ds.LoadCustomConfig(s.serviceConfig, "SocketInfo"); err != nil {
		return fmt.Errorf("unable to load 'SocketInfo' custom configuration: %s", err.Error())
	}

	lc.Infof("Socket config is: %v", s.serviceConfig.SocketInfo)

	if err := s.serviceConfig.SocketInfo.Validate(); err != nil {
		return fmt.Errorf("'SocketInfo' configuration validation failed: %s", err.Error())
	}

	s.readCommandsExecuted = gometrics.NewCounter()

	var err error
	metricsManger := ds.GetMetricsManager()
	if metricsManger != nil {
		err = metricsManger.Register(readCommandsExecutedName, s.readCommandsExecuted, nil)
	} else {
		err = errors.New("metrics manager not available")
	}

	if err != nil {
		return fmt.Errorf("unable to register metric %s: %s", readCommandsExecutedName, err.Error())
	}

	s.lc.Infof("Registered %s metric for collection when enabled", readCommandsExecutedName)

	socketInfo := s.serviceConfig.SocketInfo
	switch socketInfo.SocketType {
	case "tcp":
		{
			s.eegandfaciallistener, err = net.Listen("tcp", ":"+socketInfo.EEGAndFacialPort)
			if err != nil {
				s.lc.Errorf("Error listening on EEGAndFacialPort:", err)
			}
			go s.ListeningToClients(s.eegandfaciallistener)
			s.lc.Infof("Listening on EEGAndFacialPort: %s", socketInfo.EEGAndFacialPort)
		}
	default:
		s.lc.Info("SocketType is not TCP")
	}

	return nil
}

// deviceName: 发送get命令时的物理设备的名称
// protocals: 不清楚有什么作用
// reqs: 命令的详细内容，可以通过该参数获取请求的数值类型等等
// 返回值res: 将命令需要返回的值封装到res中，会自动包装成asyncCh发送到coredata中去
func (s *SocketDriver) HandleReadCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModels.CommandRequest) (res []*sdkModels.CommandValue, err error) {
	res = make([]*sdkModels.CommandValue, len(reqs))

	if reqs[0].DeviceResourceName == "EEGAndFacial" {
		if s.deviceInfo[deviceName].conn == nil {
			s.lc.Debugf("connection between device: %s and device-service has not been established yet", deviceName)
			return nil, nil
		}
		s.result, err = s.DecodeJsonData(s.deviceInfo[deviceName].conn)
		if err != nil {
			s.lc.Errorf("failed decoding message from device:%s, close the connection between device-service and %s", deviceName, s.deviceInfo[deviceName].deviceIP)
			s.deviceInfo[deviceName].conn.Close()
			s.deviceInfo[deviceName] = DeviceInfo{
				deviceIP: protocols["socket"]["Host"],
				conn:     nil,
			}
			return nil, err
		}

		s.lc.Infof(fmt.Sprintf("successfully received data from device : %s, ip : %s", deviceName, s.deviceInfo[deviceName].deviceIP))
		s.lc.Debugf(fmt.Sprintf("data content: %s", s.result))

		cv, err := sdkModels.NewCommandValue(reqs[0].DeviceResourceName, common.ValueTypeObject, s.result)
		if err != nil {
			s.lc.Errorf("failed NewCommandValue")
		}
		res[0] = cv

		s.readCommandsExecuted.Inc(1)

		return res, err
	}

	return nil, err
}

func (s *SocketDriver) HandleWriteCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModels.CommandRequest, params []*sdkModels.CommandValue) error {
	return nil
}

func (s *SocketDriver) Stop(force bool) error {
	// 关闭所有socket连接
	for _, v := range s.deviceInfo {
		v.conn.Close()
	}
	s.eegandfaciallistener.Close()
	return nil
}

func (s *SocketDriver) AddDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	s.lc.Infof("a new Device is added, deviceName: %s, host : %s", deviceName, protocols["socket"]["Host"])
	s.deviceInfo[deviceName] = DeviceInfo{
		deviceIP: protocols["socket"]["Host"],
		conn:     nil,
	}
	return nil
}

func (s *SocketDriver) UpdateDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	if s.deviceInfo[deviceName].deviceIP != protocols["socket"]["Host"]+":"+protocols["socket"]["Port"] {
		s.deviceInfo[deviceName].conn.Close()
		s.deviceInfo[deviceName] = DeviceInfo{
			deviceIP: protocols["socket"]["Host"],
			conn:     nil,
		}
		s.lc.Infof("Device's netInfo changed, close the connection between device-service and %s", deviceName)
	}
	return nil
}

func (s *SocketDriver) RemoveDevice(deviceName string, protocols map[string]models.ProtocolProperties) error {
	// 关闭设备关联的socket连接
	s.lc.Infof("Device %s is removed, close the connection between device-service and %s", deviceName)
	s.deviceInfo[deviceName].conn.Close()

	delete(s.deviceInfo, deviceName)
	return nil
}

func (s *SocketDriver) Discover() {
}

func (s *SocketDriver) ValidateDevice(device models.Device) error {
	protocol, ok := device.Protocols["socket"]
	if !ok {
		return errors.New("missing 'socket' protocols")
	}

	host, ok := protocol["Host"]
	if !ok {
		return errors.New("missing 'Host' information")
	} else if host == "" {
		return errors.New("host must not empty")
	}

	return nil
}

func (s *SocketDriver) ListeningToClients(listener net.Listener) {
	for {
		remoteConn, err := listener.Accept()
		index := strings.Index(remoteConn.RemoteAddr().String(), ":")
		clientIP := remoteConn.RemoteAddr().String()[:index]
		if err != nil {
			s.lc.Errorf("Failed to accept connection request from %s, failed reason: %s", clientIP, err.Error())
		}

		s.lc.Info(fmt.Sprintf("processing %s connection request", clientIP))
		s.ProcessConnectionRequest(remoteConn)
	}
}

func (s *SocketDriver) ProcessConnectionRequest(conn net.Conn) {
	index := strings.Index(conn.RemoteAddr().String(), ":")
	clientIP := conn.RemoteAddr().String()[:index]
	// 确认socket连接是否来自边缘设备
	for k, v := range s.deviceInfo {
		if v.deviceIP == clientIP {
			v.conn = conn
			s.deviceInfo[k] = v
			s.lc.Infof("successfully bind socket connection request %s with device: %s", clientIP, k)
			return
		}
	}

	conn.Close()
	s.lc.Infof("Failed to accept socket connection request %s, please confirm whether the deviceInfo has been successfully registered before connection", clientIP)
}

func (s *SocketDriver) DecodeJsonData(conn net.Conn) (map[string]interface{}, error) {
	// 创建带缓冲的读取器
	reader := bufio.NewReader(conn)

	//读取数据直到遇到 "_$"
	data, err := reader.ReadString('$')
	if err != nil {
		s.lc.Errorf("Error reading data, err reason : %s, data : %s", err, data)
		return nil, err
	}

	var jsonData map[string]interface{}

	data = strings.TrimSuffix(data, "_$")

	// 解码 JSON 数据
	err = json.Unmarshal([]byte(data), &jsonData)
	if err != nil {
		s.lc.Errorf("Error decoding JSON data, err reason : %s, data : %s", err, data)
		return nil, err
	}

	return jsonData, nil
}
