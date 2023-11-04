package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
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

// deviceName: 发送get命令时的物理设备的名称
// protocals: 不清楚有什么作用
// reqs: 命令的详细内容，可以通过该参数获取请求的数值类型等等
// 返回值res: 将命令需要返回的值封装到res中，会自动包装成asyncCh发送到coredata中去
func (s *SocketDriver) HandleReadCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModels.CommandRequest) (res []*sdkModels.CommandValue, err error) {
	res = make([]*sdkModels.CommandValue, len(reqs))

	if reqs[0].DeviceResourceName == "FacialAndEEG" {
		if s.deviceInfo[deviceName].FacialeegConn.Conn == nil {
			s.lc.Debugf("connection between device: %s and device-service has not been established yet", deviceName)
			return nil, nil
		}
		s.result, err = s.DecodeJsonData(s.deviceInfo[deviceName].FacialeegConn.Conn)
		if err != nil {
			s.lc.Errorf("failed decoding message from device:%s, close the connection between device-service and FacialAndEEG, ip : %s", deviceName, s.deviceInfo[deviceName].FacialeegConn.ClientIP)
			s.deviceInfo[deviceName].FacialeegConn.Conn.Close()
			s.deviceInfo[deviceName] = config.DeviceInfo{
				FacialeegConn: config.ConnectionInfo{
					ClientIP: s.deviceInfo[deviceName].FacialeegConn.ClientIP,
					Conn:     nil,
				},
				EyekmConn:           s.deviceInfo[deviceName].EyekmConn,
				StudentAndClassInfo: s.deviceInfo[deviceName].StudentAndClassInfo,
			}

			return nil, err
		}

		stringValues, err := s.parseStringValue(s.result["facial_eeg_collect_timestamp"])
		if err != nil {
			return nil, err
		}
		timestampValue := stringValues[0]

		s.result["student_id"] = s.deviceInfo[deviceName].StudentAndClassInfo.Student.StudentID
		s.result["class_id"] = s.deviceInfo[deviceName].StudentAndClassInfo.Class.ClassID

		s.lc.Infof(fmt.Sprintf("successfully received facial_eeg_data from device : %s, ip : %s, student name : %s, studend ID : %s , class id : %s, teacher name : %s, facial_eeg_collect_timestamp : %s",
			deviceName, s.deviceInfo[deviceName].FacialeegConn.ClientIP, s.deviceInfo[deviceName].StudentAndClassInfo.Student.StudentID,
			s.deviceInfo[deviceName].StudentAndClassInfo.Student.StudentName, s.deviceInfo[deviceName].StudentAndClassInfo.Class.ClassID,
			s.deviceInfo[deviceName].StudentAndClassInfo.Class.TeacherName, timestampValue))
		s.lc.Debugf(fmt.Sprintf("data content: %s", s.result))

		cv, err := sdkModels.NewCommandValue(reqs[0].DeviceResourceName, common.ValueTypeObject, s.result)
		if err != nil {
			s.lc.Errorf("failed NewCommandValue")
		}
		res[0] = cv

		s.readCommandsExecuted.Inc(1)

		return res, err
	} else if reqs[0].DeviceResourceName == "EyeAndKm" {
		// do nothing now
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

func (s *SocketDriver) RegisterStudentAndClassInfo(studentAndClass config.StudentAndClassInfo, url string, timeOut int) error {
	// 将数据转换为 json
	jsonData, err := json.Marshal(studentAndClass)
	if err != nil {
		s.lc.Errorf("Error occurred while encoding studentAndClass data: %v", err)
		return err
	}

	// 创建新的 http POST 请求
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		s.lc.Errorf("Error occurred while creating HTTP request: %v", err)
		return err
	}

	// 设置 header 信息，告知服务器端发送的是 json 数据格式
	req.Header.Set("Content-Type", "application/json")

	// 创建一个带有超时时间的 context
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeOut)*time.Second)
	defer cancel()

	// 将创建的 context 附加到 http request 上
	req = req.WithContext(ctx)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		s.lc.Errorf("Error occurred while sending HTTP request: %v", err)
		return err
	}

	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode == http.StatusOK {
		s.lc.Infof("Successfully register student and class info, http response's StatusCode : %d", resp.StatusCode)
		return nil
	} else {
		return errors.New("failed to register student info")
	}
}

func (pf *SocketDriver) parseStringValue(values ...interface{}) ([]string, error) {
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

func (s *SocketDriver) HandleWriteCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModels.CommandRequest, params []*sdkModels.CommandValue) error {
	return nil
}

func (s *SocketDriver) AddDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	s.lc.Infof("a new Device is added, deviceName: %s, FacialEEGIP : %s, EyeKmIP : %s", deviceName, protocols["socket"]["FacialEEGIP"], protocols["socket"]["EyeKmIP"])
	s.deviceInfo[deviceName] = config.DeviceInfo{
		FacialeegConn: config.ConnectionInfo{
			ClientIP: protocols["socket"]["FacialEEGIP"],
			Conn:     nil,
		},
		EyekmConn: config.ConnectionInfo{
			ClientIP: protocols["socket"]["EyeKmIP"],
			Conn:     nil,
		},
		StudentAndClassInfo: config.StudentAndClassInfo{
			Student: config.StudentInfo{
				StudentID:   protocols["StudentAndClassInfo"]["student_id"],
				StudentName: protocols["StudentAndClassInfo"]["student_name"],
			},
			Class: config.ClassInfo{
				ClassID:     protocols["StudentAndClassInfo"]["class_id"],
				TeacherName: protocols["StudentAndClassInfo"]["teacher_name"],
			},
		},
	}
	if err := s.RegisterStudentAndClassInfo(s.deviceInfo[deviceName].StudentAndClassInfo, s.serviceConfig.SocketInfo.StudentAndClassInfoPostURL, s.serviceConfig.SocketInfo.TimeOut); err != nil {
		s.lc.Errorf("Failed to register student and class info to database: %v", err)
		return err
	}
	return nil
}

func (s *SocketDriver) UpdateDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	// 默认学生信息不允许更新, 只检测IP信息是否有变更
	var newFacialEEGConn, newEyeKmConn config.ConnectionInfo

	if s.deviceInfo[deviceName].FacialeegConn.ClientIP != protocols["socket"]["FacialEEGIP"] {
		newFacialEEGConn = config.ConnectionInfo{
			ClientIP: protocols["socket"]["FacialEEGIP"],
			Conn:     nil,
		}
		s.deviceInfo[deviceName].FacialeegConn.Conn.Close()
		s.lc.Infof("Device's FacialEEGIP changed, close the connection between device-service and %s", deviceName)
	} else {
		newFacialEEGConn = s.deviceInfo[deviceName].FacialeegConn
	}

	if s.deviceInfo[deviceName].EyekmConn.ClientIP != protocols["socket"]["EyeKmIP"] {
		newEyeKmConn = config.ConnectionInfo{
			ClientIP: protocols["socket"]["EyeKmIP"],
			Conn:     nil,
		}

		s.deviceInfo[deviceName].EyekmConn.Conn.Close()
		s.lc.Infof("Device's EyeKmIP changed, close the connection between device-service and %s", deviceName)
	} else {
		newEyeKmConn = s.deviceInfo[deviceName].EyekmConn
	}

	s.deviceInfo[deviceName] = config.DeviceInfo{
		newFacialEEGConn,
		newEyeKmConn,
		s.deviceInfo[deviceName].StudentAndClassInfo,
	}

	return nil
}

func (s *SocketDriver) RemoveDevice(deviceName string, protocols map[string]models.ProtocolProperties) error {
	// 关闭设备关联的socket连接
	s.lc.Infof("Device %s is removed, close the connection between device-service and %s", deviceName)
	s.deviceInfo[deviceName].FacialeegConn.Conn.Close()
	s.deviceInfo[deviceName].EyekmConn.Conn.Close()

	delete(s.deviceInfo, deviceName)
	return nil
}

func (s *SocketDriver) ValidateDevice(device models.Device) error {
	// Validate socket connect info
	socketProtocol, ok := device.Protocols["socket"]
	if !ok || socketProtocol["FacialEEGIP"] == "" || socketProtocol["EyeKmIP"] == "" {
		return errors.New("protocols missing 'socket' part or some fields in 'socket' are empty")
	}

	// Validate student info
	StudentAndClassInfo, ok := device.Protocols["StudentAndClassInfo"]
	if !ok || StudentAndClassInfo["student_id"] == "" || StudentAndClassInfo["student_name"] == "" || StudentAndClassInfo["class_id"] == "" || StudentAndClassInfo["teacher_name"] == "" {
		return errors.New("protocols missing 'StudentAndClassInfo' part or some fields in 'StudentAndClassInfo' are empty")
	}
	return nil
}

func (s *SocketDriver) Stop(force bool) error {
	// 关闭所有socket连接
	for _, v := range s.deviceInfo {
		v.FacialeegConn.Conn.Close()
		v.EyekmConn.Conn.Close()
	}
	s.socketServiceListener.Close()
	s.socketServiceListener.Close()
	return nil
}

func (s *SocketDriver) Discover() {
}
