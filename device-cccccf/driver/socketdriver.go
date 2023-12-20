package driver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/edgexfoundry/device-cccccf/config"
	"github.com/edgexfoundry/device-sdk-go/v2/pkg/interfaces"
	sdkModels "github.com/edgexfoundry/device-sdk-go/v2/pkg/models"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/models"

	gometrics "github.com/rcrowley/go-metrics"
)

const readCommandsExecutedName = "ReadCommandsExecuted"

type SocketDriver struct {
	lc                    logger.LoggingClient
	asyncCh               chan<- *sdkModels.AsyncValues
	deviceCh              chan<- []sdkModels.DiscoveredDevice
	deviceInfo            sync.Map
	readCommandsExecuted  gometrics.Counter
	serviceConfig         *config.ServiceConfig
	socketServiceListener net.Listener
}

func (s *SocketDriver) Initialize(lc logger.LoggingClient, asyncCh chan<- *sdkModels.AsyncValues, deviceCh chan<- []sdkModels.DiscoveredDevice) error {
	s.lc = lc
	s.asyncCh = asyncCh
	s.deviceCh = deviceCh
	s.serviceConfig = &config.ServiceConfig{}

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
			s.socketServiceListener, err = net.Listen("tcp", ":"+socketInfo.SocketServerPort)
			if err != nil {
				s.lc.Errorf("Error listening on SocketServerPort:", err)
			}
			go s.ListeningToClients(s.socketServiceListener)
			s.lc.Infof("Listening on SocketServerPort: %s", socketInfo.SocketServerPort)
		}
	default:
		s.lc.Info("SocketType is not TCP")
	}

	return nil
}

// reqs: 命令的详细内容，可以通过该参数获取请求的数值类型等等
// 返回值res: 将命令需要返回的值封装到res中，会自动包装成asyncCh发送到coredata中去
func (s *SocketDriver) HandleReadCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModels.CommandRequest) (res []*sdkModels.CommandValue, err error) {
	res = make([]*sdkModels.CommandValue, len(reqs))

	deviceInfoInterface, ok := s.deviceInfo.Load(deviceName)
	if !ok {
		s.lc.Errorf("failed to find device info for device:%s", deviceName)
		return nil, fmt.Errorf("failed to find device info for device: %s", deviceName)
	}
	deviceInfo := deviceInfoInterface.(config.DeviceInfo)

	// 在函数内创建result map用于存储结果
	if reqs[0].DeviceResourceName == "FacialAndEEG" {
		if deviceInfo.FacialeegConn.Conn == nil {
			s.lc.Debugf("FacialAndEEG connection between device: %s and device-service has not been established yet", deviceName)
			return nil, nil
		}
		result, err := s.DecodeJsonData(deviceInfo.FacialeegConn.Conn)
		if err != nil {
			connInfo, _ := s.deviceInfo.Load(deviceName)
			currentDeviceInfo := connInfo.(config.DeviceInfo)
			s.lc.Errorf("failed decoding message from device:%s, close the connection between device-service and FacialAndEEG, ip : %s", deviceName, currentDeviceInfo.FacialeegConn.ClientIP)
			if currentDeviceInfo.FacialeegConn.Conn != nil {
				currentDeviceInfo.FacialeegConn.Conn.Close()
			}

			// 修改连接信息并且重新保存到sync.Map中
			currentDeviceInfo.FacialeegConn.Conn = nil
			s.deviceInfo.Store(deviceName, currentDeviceInfo)

			return nil, err
		}

		result["student_id"] = deviceInfo.DeviceDataLabel.StudentID
		if deviceInfo.DeviceDataLabel.TaskID != "" {
			result["task_id"] = deviceInfo.DeviceDataLabel.TaskID
		}
		result["facial_eeg_collect_timestamp"] = generateTimestamp()

		stringValues, err := parseStringValue(result["seq"])
		if err != nil {
			return nil, err
		}
		seqValue := stringValues[0]

		s.lc.Infof(fmt.Sprintf("successfully received facial_eeg_data from device : %s, ip : %s, studend ID : %s, task ID : %s, seq : %s, facial_eeg_collect_timestamp : %s",
			deviceName, deviceInfo.FacialeegConn.ClientIP, result["student_id"], deviceInfo.DeviceDataLabel.TaskID, seqValue, result["facial_eeg_collect_timestamp"]))
		s.lc.Debugf(fmt.Sprintf("data content: %s", result))

		cv, err := sdkModels.NewCommandValue(reqs[0].DeviceResourceName, common.ValueTypeObject, result)
		if err != nil {
			s.lc.Errorf("failed NewCommandValue")
		}
		res[0] = cv

		s.readCommandsExecuted.Inc(1)

		return res, err
	} else if reqs[0].DeviceResourceName == "KeyboardAndMouse" {
		if deviceInfo.KeyboardMouse.Conn == nil {
			s.lc.Debugf("Eyekm connection between device: %s and device-service has not been established yet", deviceName)
			return nil, nil
		}
		result, err := s.DecodeJsonData(deviceInfo.KeyboardMouse.Conn)
		if err != nil {
			connInfo, _ := s.deviceInfo.Load(deviceName)
			currentDeviceInfo := connInfo.(config.DeviceInfo)
			s.lc.Errorf("failed decoding message from device:%s, close the connection between device-service and KeyboardMouse, ip : %s", deviceName, currentDeviceInfo.KeyboardMouse.ClientIP)
			if currentDeviceInfo.KeyboardMouse.Conn != nil {
				currentDeviceInfo.KeyboardMouse.Conn.Close()
			}

			// 修改连接信息并且重新保存到sync.Map中
			currentDeviceInfo.KeyboardMouse.Conn = nil
			s.deviceInfo.Store(deviceName, currentDeviceInfo)

			return nil, err
		}

		result["student_id"] = deviceInfo.DeviceDataLabel.StudentID
		if deviceInfo.DeviceDataLabel.TaskID != "" {
			result["task_id"] = deviceInfo.DeviceDataLabel.TaskID
		}
		result["keyboard_mouse_collect_timestamp"] = generateTimestamp()

		stringValues, err := parseStringValue(result["seq"])
		if err != nil {
			return nil, err
		}
		seqValue := stringValues[0]

		s.lc.Infof(fmt.Sprintf("successfully received keyboard_mouse_data from device : %s, ip : %s, studend ID : %s, task ID : %s, seq : %s, keyboard_mouse_collect_timestamp : %s",
			deviceName, deviceInfo.KeyboardMouse.ClientIP, result["student_id"], deviceInfo.DeviceDataLabel.TaskID, seqValue, result["keyboard_mouse_collect_timestamp"]))
		s.lc.Debugf(fmt.Sprintf("data content: %s", result))

		cv, err := sdkModels.NewCommandValue(reqs[0].DeviceResourceName, common.ValueTypeObject, result)
		if err != nil {
			s.lc.Errorf("failed NewCommandValue")
		}
		res[0] = cv

		s.readCommandsExecuted.Inc(1)

		return res, err
	}

	return nil, err
}

func (s *SocketDriver) ListeningToClients(listener net.Listener) {
	for {
		remoteConn, err := listener.Accept()
		index := strings.Index(remoteConn.RemoteAddr().String(), ":")
		ClientIP := remoteConn.RemoteAddr().String()[:index]
		if err != nil {
			s.lc.Errorf("Failed to accept connection request from %s, failed reason: %s", ClientIP, err.Error())
		}

		s.lc.Info(fmt.Sprintf("processing %s connection request", ClientIP))
		s.ProcessConnectionRequest(remoteConn)
	}
}

func (s *SocketDriver) ProcessConnectionRequest(conn net.Conn) {
	clientIP := conn.RemoteAddr().String()
	// 确认socket连接是否来自边缘设备
	var deviceFound bool
	s.deviceInfo.Range(func(k, v interface{}) bool {
		deviceName := k.(string)
		deviceInfo := v.(config.DeviceInfo)

		if deviceInfo.FacialeegConn.ClientIP == clientIP {
			deviceInfo.FacialeegConn.Conn = conn
			s.deviceInfo.Store(deviceName, deviceInfo)
			s.lc.Infof("successfully bind FacialAndEEG connection request %s with device: %s", clientIP, deviceName)
			deviceFound = true
			return false // 停止迭代
		} else if deviceInfo.KeyboardMouse.ClientIP == clientIP {
			deviceInfo.KeyboardMouse.Conn = conn
			s.deviceInfo.Store(deviceName, deviceInfo)
			s.lc.Infof("successfully bind KeyboardAndMouse connection request %s with device: %s", clientIP, deviceName)
			deviceFound = true
			return false // 停止迭代
		}
		return true // 继续迭代
	})

	if !deviceFound {
		conn.Close()
		s.lc.Infof("Failed to accept socket connection request %s, please confirm whether the deviceInfo has been successfully registered before connection", clientIP)
	}
}

func (s *SocketDriver) DecodeJsonData(conn net.Conn) (map[string]interface{}, error) {
	// 创建带缓冲的读取器
	reader := bufio.NewReader(conn)

	//读取数据直到遇到 "_$"
	data, err := reader.ReadString('$')
	if err != nil {
		s.lc.Errorf("Error reading data, err reason : %s", err)
		return nil, err
	}

	var jsonData map[string]interface{}

	data = strings.TrimSuffix(data, "_$")

	// 解码 JSON 数据
	err = json.Unmarshal([]byte(data), &jsonData)
	if err != nil {
		s.lc.Errorf("Error decoding JSON data, err reason : %s", err)
		return nil, err
	}

	return jsonData, nil
}

func parseStringValue(values ...interface{}) ([]string, error) {
	var strs []string
	for _, value := range values {
		switch v := value.(type) {
		case string:
			strs = append(strs, v)
		default:
			return nil, fmt.Errorf("value is not string, data content: %s", value)
		}
	}

	return strs, nil
}

func generateTimestamp() string {
	t := time.Now()
	return t.Format(time.RFC3339)
}

func (s *SocketDriver) HandleWriteCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModels.CommandRequest, params []*sdkModels.CommandValue) error {
	return nil
}

func (s *SocketDriver) AddDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	facialEEGIP := protocols["socket"]["deviceIP"] + ":" + s.serviceConfig.SocketInfo.FacialAndEEGPort
	eyeKmIP := protocols["socket"]["deviceIP"] + ":" + s.serviceConfig.SocketInfo.KeyboardAndMousePort
	deviceDataLabel := config.EducationInfo{
		StudentID: protocols["deviceDataLabel"]["student_id"],
		TaskID:    protocols["deviceDataLabel"]["task_id"],
	}

	newDeviceInfo := config.DeviceInfo{
		FacialeegConn: config.ConnectionInfo{
			ClientIP: facialEEGIP,
			Conn:     nil,
		},
		KeyboardMouse: config.ConnectionInfo{
			ClientIP: eyeKmIP,
			Conn:     nil,
		},
		DeviceDataLabel: deviceDataLabel,
	}

	s.deviceInfo.Store(deviceName, newDeviceInfo)
	s.lc.Infof("a new Device is added, deviceName: %s, FacialEEGIP : %s, EyeKmIP : %s", deviceName, facialEEGIP, eyeKmIP)
	return nil
}

func (s *SocketDriver) UpdateDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	clientIP := protocols["socket"]["deviceIP"]
	connInfo, ok := s.deviceInfo.Load(deviceName)
	if !ok {
		return fmt.Errorf("device %s not found", deviceName)
	}
	currentDeviceInfo := connInfo.(config.DeviceInfo)
	deviceIP, _, err := net.SplitHostPort(currentDeviceInfo.FacialeegConn.ClientIP)
	if err != nil {
		return err
	}

	if clientIP != deviceIP {
		// 检查IP信息是否有更新, 如果有更新则断开原先的连接, 并且设置新连接为空
		if currentDeviceInfo.FacialeegConn.Conn != nil {
			currentDeviceInfo.FacialeegConn.Conn.Close()
		}
		if currentDeviceInfo.KeyboardMouse.Conn != nil {
			currentDeviceInfo.KeyboardMouse.Conn.Close()
		}

		// 更新IP地址并存储到sync.Map中
		currentDeviceInfo.FacialeegConn = config.ConnectionInfo{
			ClientIP: clientIP + ":" + s.serviceConfig.SocketInfo.FacialAndEEGPort,
			Conn:     nil,
		}
		currentDeviceInfo.KeyboardMouse = config.ConnectionInfo{
			ClientIP: clientIP + ":" + s.serviceConfig.SocketInfo.KeyboardAndMousePort,
			Conn:     nil,
		}
		currentDeviceInfo.DeviceDataLabel.TaskID = protocols["deviceDataLabel"]["task_id"]

		s.deviceInfo.Store(deviceName, currentDeviceInfo)

		s.lc.Infof("DeviceIP changed, close the connection between device-service and %s, FacialEEGIP : %s, EyeKmIP : %s, student_id : %s, task_id : %s",
			deviceName, currentDeviceInfo.FacialeegConn.ClientIP, currentDeviceInfo.KeyboardMouse.ClientIP,
			currentDeviceInfo.DeviceDataLabel.StudentID, currentDeviceInfo.DeviceDataLabel.TaskID)
	} else if currentDeviceInfo.DeviceDataLabel.TaskID != protocols["deviceDataLabel"]["task_id"] {
		// 检查EducationInfo是否有更新, StudentID默认不会更改, 所以只需要检测TaskID部分
		currentDeviceInfo.DeviceDataLabel.TaskID = protocols["deviceDataLabel"]["task_id"]
		s.deviceInfo.Store(deviceName, currentDeviceInfo)

		s.lc.Infof("EducationInfo changed, student_id : %s, task_id : %s", currentDeviceInfo.DeviceDataLabel.StudentID, currentDeviceInfo.DeviceDataLabel.TaskID)
	} else {
		s.lc.Infof("ClientIP and EducationInfo have not changed")
	}

	return nil
}

func (s *SocketDriver) RemoveDevice(deviceName string, protocols map[string]models.ProtocolProperties) error {
	// 从sync.Map中获取设备信息
	deviceInfoInterface, ok := s.deviceInfo.Load(deviceName)
	if !ok {
		s.lc.Infof("Device %s does not exist or has already been removed", deviceName)
		return nil
	}
	deviceInfo := deviceInfoInterface.(config.DeviceInfo)

	// 关闭设备关联的socket连接
	s.lc.Infof("Device %s is removed, close the connection between device-service and %s", deviceName, deviceName)
	if deviceInfo.FacialeegConn.Conn != nil {
		deviceInfo.FacialeegConn.Conn.Close()
	}
	if deviceInfo.KeyboardMouse.Conn != nil {
		deviceInfo.KeyboardMouse.Conn.Close()
	}

	// 从sync.Map中删除设备信息
	s.deviceInfo.Delete(deviceName)

	return nil
}

func (s *SocketDriver) ValidateDevice(device models.Device) error {
	// Validate socket connect info
	socketProtocol, ok := device.Protocols["socket"]
	if !ok || socketProtocol["deviceIP"] == "" {
		return errors.New("protocols missing 'socket' part or some fields in 'socket' are empty")
	}

	// Validate education info
	educationInfo, ok := device.Protocols["deviceDataLabel"]
	if !ok || educationInfo["student_id"] == "" {
		return errors.New("protocols missing 'deviceDataLabel' part or some fields in 'deviceDataLabel' are empty")
	}
	return nil
}

func (s *SocketDriver) Stop(force bool) error {
	// 关闭所有socket连接
	s.deviceInfo.Range(func(_, value interface{}) bool {
		deviceInfo := value.(config.DeviceInfo)
		if deviceInfo.FacialeegConn.Conn != nil {
			deviceInfo.FacialeegConn.Conn.Close()
		}
		if deviceInfo.KeyboardMouse.Conn != nil {
			deviceInfo.KeyboardMouse.Conn.Close()
		}
		return true // 继续迭代
	})

	// 关闭socket服务监听器
	if s.socketServiceListener != nil {
		err := s.socketServiceListener.Close()
		if err != nil {
			s.lc.Errorf("Failed to close socket service listener: %v", err)
			return err
		}
	}

	return nil
}

func (s *SocketDriver) Discover() {
}
