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
	mtx                   sync.RWMutex
	result                map[string]interface{}
	deviceInfo            map[string]config.DeviceInfo
	readCommandsExecuted  gometrics.Counter
	serviceConfig         *config.ServiceConfig
	socketServiceListener net.Listener
}

func (s *SocketDriver) Initialize(lc logger.LoggingClient, asyncCh chan<- *sdkModels.AsyncValues, deviceCh chan<- []sdkModels.DiscoveredDevice) error {
	s.lc = lc
	s.asyncCh = asyncCh
	s.deviceCh = deviceCh
	s.serviceConfig = &config.ServiceConfig{}

	s.deviceInfo = make(map[string]config.DeviceInfo)

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
	// 使用读锁进行数据读取
	s.mtx.RLock()
	// 获取需要的设备信息
	deviceInfo := s.deviceInfo[deviceName]
	s.mtx.RUnlock()

	res = make([]*sdkModels.CommandValue, len(reqs))

	if reqs[0].DeviceResourceName == "FacialAndEEG" {
		if deviceInfo.FacialeegConn.Conn == nil {
			s.lc.Debugf("FacialAndEEG connection between device: %s and device-service has not been established yet", deviceName)
			return nil, nil
		}
		s.result, err = s.DecodeJsonData(deviceInfo.FacialeegConn.Conn)
		if err != nil {
			// 添加写锁
			s.mtx.Lock()
			defer s.mtx.Unlock()

			s.lc.Errorf("failed decoding message from device:%s, close the connection between device-service and FacialAndEEG, ip : %s", deviceName, s.deviceInfo[deviceName].FacialeegConn.ClientIP)
			if s.deviceInfo[deviceName].FacialeegConn.Conn != nil {
				s.deviceInfo[deviceName].FacialeegConn.Conn.Close()
			}

			s.deviceInfo[deviceName] = config.DeviceInfo{
				FacialeegConn: config.ConnectionInfo{
					ClientIP: s.deviceInfo[deviceName].FacialeegConn.ClientIP,
					Conn:     nil,
				},
				EyekmConn:       s.deviceInfo[deviceName].EyekmConn,
				DeviceDataLabel: s.deviceInfo[deviceName].DeviceDataLabel,
			}

			return nil, err
		}

		s.result["student_id"] = deviceInfo.DeviceDataLabel.StudentID
		if deviceInfo.DeviceDataLabel.TaskID != "" {
			s.result["task_id"] = deviceInfo.DeviceDataLabel.TaskID
		}
		s.result["facial_eeg_collect_timestamp"] = generateTimestamp()

		stringValues, err := parseStringValue(s.result["seq"])
		if err != nil {
			return nil, err
		}
		seqValue := stringValues[0]

		s.lc.Infof(fmt.Sprintf("successfully received facial_eeg_data from device : %s, ip : %s, studend ID : %s, task ID : %s, seq : %s, facial_eeg_collect_timestamp : %s",
			deviceName, deviceInfo.FacialeegConn.ClientIP, s.result["student_id"], deviceInfo.DeviceDataLabel.TaskID, seqValue, s.result["facial_eeg_collect_timestamp"]))
		s.lc.Debugf(fmt.Sprintf("data content: %s", s.result))

		cv, err := sdkModels.NewCommandValue(reqs[0].DeviceResourceName, common.ValueTypeObject, s.result)
		if err != nil {
			s.lc.Errorf("failed NewCommandValue")
		}
		res[0] = cv

		s.readCommandsExecuted.Inc(1)

		return res, err
	} else if reqs[0].DeviceResourceName == "EyeAndKm" {
		if deviceInfo.EyekmConn.Conn == nil {
			s.lc.Debugf("Eyekm connection between device: %s and device-service has not been established yet", deviceName)
			return nil, nil
		}
		s.result, err = s.DecodeJsonData(deviceInfo.EyekmConn.Conn)
		if err != nil {
			// 添加写锁
			s.mtx.Lock()
			defer s.mtx.Unlock()

			s.lc.Errorf("failed decoding message from device:%s, close the connection between device-service and EyekmConn, ip : %s", deviceName, s.deviceInfo[deviceName].EyekmConn.ClientIP)
			if s.deviceInfo[deviceName].EyekmConn.Conn != nil {
				s.deviceInfo[deviceName].EyekmConn.Conn.Close()
			}

			s.deviceInfo[deviceName] = config.DeviceInfo{
				FacialeegConn: s.deviceInfo[deviceName].FacialeegConn,
				EyekmConn: config.ConnectionInfo{
					ClientIP: s.deviceInfo[deviceName].EyekmConn.ClientIP,
					Conn:     nil,
				},
				DeviceDataLabel: s.deviceInfo[deviceName].DeviceDataLabel,
			}

			return nil, err
		}

		s.result["student_id"] = deviceInfo.DeviceDataLabel.StudentID
		if deviceInfo.DeviceDataLabel.TaskID != "" {
			s.result["task_id"] = deviceInfo.DeviceDataLabel.TaskID
		}
		s.result["eye_km_collect_timestamp"] = generateTimestamp()

		stringValues, err := parseStringValue(s.result["seq"])
		if err != nil {
			return nil, err
		}
		seqValue := stringValues[0]

		s.lc.Infof(fmt.Sprintf("successfully received eye_km_data from device : %s, ip : %s, studend ID : %s, task ID : %s, seq : %s, eye_km_collect_timestamp : %s",
			deviceName, deviceInfo.EyekmConn.ClientIP, s.result["student_id"], deviceInfo.DeviceDataLabel.TaskID, seqValue, s.result["eye_km_collect_timestamp"]))
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
	ClientIP := conn.RemoteAddr().String()
	// 确认socket连接是否来自边缘设备
	for k, v := range s.deviceInfo {
		if v.FacialeegConn.ClientIP == ClientIP {
			v.FacialeegConn.Conn = conn
			s.deviceInfo[k] = v
			s.lc.Infof("successfully bind FacialAndEEG connection request %s with device: %s", ClientIP, k)
			return
		} else if v.EyekmConn.ClientIP == ClientIP {
			v.EyekmConn.Conn = conn
			s.deviceInfo[k] = v
			s.lc.Infof("successfully bind EyeAndKm connection request %s with device: %s", ClientIP, k)
			return
		}
	}

	conn.Close()
	s.lc.Infof("Failed to accept socket connection request %s, please confirm whether the deviceInfo has been successfully registered before connection", ClientIP)
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
	// 添加写锁
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.deviceInfo[deviceName] = config.DeviceInfo{
		FacialeegConn: config.ConnectionInfo{
			ClientIP: protocols["socket"]["deviceIP"] + ":" + s.serviceConfig.SocketInfo.FacialAndEEGPort,
			Conn:     nil,
		},
		EyekmConn: config.ConnectionInfo{
			ClientIP: protocols["socket"]["deviceIP"] + ":" + s.serviceConfig.SocketInfo.EyeAndKmPort,
			Conn:     nil,
		},
		DeviceDataLabel: config.EducationInfo{
			StudentID: protocols["deviceDataLabel"]["student_id"],
			TaskID:    protocols["deviceDataLabel"]["task_id"],
		},
	}
	s.lc.Infof("a new Device is added, deviceName: %s, FacialEEGIP : %s, EyeKmIP : %s", deviceName, s.deviceInfo[deviceName].FacialeegConn.ClientIP, s.deviceInfo[deviceName].EyekmConn.ClientIP)
	return nil
}

func (s *SocketDriver) UpdateDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	// 添加写锁
	s.mtx.Lock()
	defer s.mtx.Unlock()

	clientIP := protocols["socket"]["deviceIP"]
	deviceIP, _, err := net.SplitHostPort(s.deviceInfo[deviceName].FacialeegConn.ClientIP)
	if err != nil {
		return err
	}

	if clientIP != deviceIP {
		// 检查IP信息是否有更新, 如果有更新则断开原先的连接, 并且设置新连接为空
		s.deviceInfo[deviceName] = config.DeviceInfo{
			FacialeegConn: config.ConnectionInfo{
				ClientIP: clientIP + ":" + s.serviceConfig.SocketInfo.FacialAndEEGPort,
				Conn:     nil,
			},
			EyekmConn: config.ConnectionInfo{
				ClientIP: clientIP + ":" + s.serviceConfig.SocketInfo.EyeAndKmPort,
				Conn:     nil,
			},
			DeviceDataLabel: config.EducationInfo{
				StudentID: s.deviceInfo[deviceName].DeviceDataLabel.StudentID,
				TaskID:    protocols["deviceDataLabel"]["task_id"],
			},
		}
		if s.deviceInfo[deviceName].FacialeegConn.Conn != nil {
			s.deviceInfo[deviceName].FacialeegConn.Conn.Close()
		}
		if s.deviceInfo[deviceName].EyekmConn.Conn != nil {
			s.deviceInfo[deviceName].EyekmConn.Conn.Close()
		}
		s.lc.Infof("DeviceIP changed, close the connection between device-service and %s, FacialEEGIP : %s, EyeKmIP : %s, student_id : %s, task_id : %s",
			deviceName, s.deviceInfo[deviceName].FacialeegConn.ClientIP, s.deviceInfo[deviceName].EyekmConn.ClientIP,
			s.deviceInfo[deviceName].DeviceDataLabel.StudentID, s.deviceInfo[deviceName].DeviceDataLabel.TaskID)
	} else if s.deviceInfo[deviceName].DeviceDataLabel.TaskID != protocols["deviceDataLabel"]["task_id"] {
		// 检查EducationInfo是否有更新, StudentID默认不会更改, 所以只需要检测TaskID部分
		s.deviceInfo[deviceName] = config.DeviceInfo{
			FacialeegConn: s.deviceInfo[deviceName].FacialeegConn,
			EyekmConn:     s.deviceInfo[deviceName].EyekmConn,
			DeviceDataLabel: config.EducationInfo{
				StudentID: s.deviceInfo[deviceName].DeviceDataLabel.StudentID,
				TaskID:    protocols["deviceDataLabel"]["task_id"],
			},
		}
		s.lc.Infof("EducationInfo changed, student_id : %s, task_id : %s", s.deviceInfo[deviceName].DeviceDataLabel.StudentID, s.deviceInfo[deviceName].DeviceDataLabel.TaskID)
	} else {
		s.lc.Infof("ClientIP and EducationInfo have not changed")
	}

	return nil
}

func (s *SocketDriver) RemoveDevice(deviceName string, protocols map[string]models.ProtocolProperties) error {
	// 添加写锁
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// 关闭设备关联的socket连接
	s.lc.Infof("Device %s is removed, close the connection between device-service and %s", deviceName)
	if s.deviceInfo[deviceName].FacialeegConn.Conn != nil {
		s.deviceInfo[deviceName].FacialeegConn.Conn.Close()
	}
	if s.deviceInfo[deviceName].EyekmConn.Conn != nil {
		s.deviceInfo[deviceName].EyekmConn.Conn.Close()
	}

	delete(s.deviceInfo, deviceName)
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
	// 添加写锁
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// 关闭所有socket连接
	for _, v := range s.deviceInfo {
		if v.FacialeegConn.Conn != nil {
			v.FacialeegConn.Conn.Close()
		}
		if v.EyekmConn.Conn != nil {
			v.EyekmConn.Conn.Close()
		}
	}
	s.socketServiceListener.Close()
	return nil
}

func (s *SocketDriver) Discover() {
}
