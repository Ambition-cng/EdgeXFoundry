package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/edgexfoundry/app-functions-sdk-go/v2/pkg/interfaces"

	"github.com/edgexfoundry/go-mod-core-contracts/v2/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/dtos"

	"github.com/edgexfoundry/app-cccccf/config"
	pb_encryption "github.com/edgexfoundry/app-cccccf/protobuf/encryption"
	pb_model "github.com/edgexfoundry/app-cccccf/protobuf/model"
)

func Newpipelinefunction(appServiceConfig config.APPServiceConfig) *pipelinefunction {
	return &pipelinefunction{
		appServiceConfig: appServiceConfig,
	}
}

type pipelinefunction struct {
	appServiceConfig config.APPServiceConfig
}

func (pf *pipelinefunction) LogEventDetails(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("LogEventDetails called in pipeline '%s'", ctx.PipelineId())

	if data == nil {
		// Go here for details on Error Handle: https://docs.edgexfoundry.org/latest/microservices/application/ErrorHandling/
		return false, fmt.Errorf("function LogEventDetails in pipeline '%s': No Data Received", ctx.PipelineId())
	}

	event, ok := data.(dtos.Event)
	if !ok {
		return false, fmt.Errorf("function LogEventDetails in pipeline '%s', type received is not an Event", ctx.PipelineId())
	}

	lc.Debugf("Event received in pipeline '%s': ID=%s, Device=%s, and ReadingCount=%d",
		ctx.PipelineId(),
		event.Id,
		event.DeviceName,
		len(event.Readings))
	for index, reading := range event.Readings {
		switch strings.ToLower(reading.ValueType) {
		case strings.ToLower(common.ValueTypeBinary):
			lc.Debugf(
				"Reading #%d received in pipeline '%s' with ID=%s, Resource=%s, ValueType=%s, MediaType=%s and BinaryValue of size=`%d`",
				index+1,
				ctx.PipelineId(),
				reading.Id,
				reading.ResourceName,
				reading.ValueType,
				reading.MediaType,
				len(reading.Value))
		case strings.ToLower(common.ValueTypeObject):
			lc.Debugf(
				"Reading #%d received in pipeline '%s' with ID=%s, Resource=%s, ValueType=%s, MediaType=%s and ObjectValue accept",
				index+1,
				ctx.PipelineId(),
				reading.Id,
				reading.ResourceName,
				reading.ValueType,
				reading.MediaType)

		default:
			lc.Debugf("Reading #%d received in pipeline '%s' with ID=%s, Resource=%s, ValueType=%s, Value=`%s`",
				index+1,
				ctx.PipelineId(),
				reading.Id,
				reading.ResourceName,
				reading.ValueType,
				reading.Value)
		}
	}

	return true, event
}

func (pf *pipelinefunction) ProcessAndSaveData(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("ProcessAndSaveData called in pipeline '%s'", ctx.PipelineId())

	if data == nil {
		return false, fmt.Errorf("function ProcessAndSaveData in pipeline '%s': No Data Received", ctx.PipelineId())
	}
	event, ok := data.(dtos.Event)
	if !ok {
		return false, fmt.Errorf("function ProcessAndSaveData in pipeline '%s', type received is not an Event", ctx.PipelineId())
	}

	var result map[string]interface{}
	var resourcename string
	// get data from devcie service, for-loop only executes once
	for _, reading := range event.Readings {
		resourcename = reading.ResourceName
		result, ok = reading.ObjectReading.ObjectValue.(map[string]interface{})
		if !ok {
			fmt.Println("Failed to convert ObjectReading to map[string]string")
		}
		lc.Debugf("success to convert ObjectReading to map[string]interface{}")
	}

	var pointerTaskID *int = nil
	// 判断result中是否存在task_id字段
	if strValue, ok := result["task_id"].(string); ok {
		// 将task_id解析为int类型的值
		taskID, err := strconv.Atoi(strValue)
		if err != nil {
			return false, fmt.Errorf("error converting value to int: %v", err)
		}
		pointerTaskID = &taskID
	}

	if resourcename == "FacialAndEEG" {
		stringValues, err := parseStringValue(result["seq"], result["student_id"], result["facial_eeg_collect_timestamp"], result["image_data"], result["eeg_data"])
		if err != nil {
			return false, err
		}
		seqValue, studentIDValue, facial_eeg_collect_timestamp, imageValue, eegValue := stringValues[0], stringValues[1], stringValues[2], stringValues[3], stringValues[4]
		eye_tracking_collect_timestamp := facial_eeg_collect_timestamp

		var facial_eeg_model_result, eye_tracking_mode_result string
		functionSwitch := pf.appServiceConfig.FunctionSwitch
		if functionSwitch.ProcessFacialAndEEGData || functionSwitch.ProcessEyeTrackingData || functionSwitch.EncryptImageData || functionSwitch.EncryptEEGData {
			var wg sync.WaitGroup
			FacialAndEEGResultChan := make(chan string, 1)
			EyeTrackingResultChan := make(chan string, 1)
			imageEncryptionResultChan := make(chan string, 1)
			eegEncryptionResultChan := make(chan string, 1)
			errorChan := make(chan error, 4)

			// 异步调用sendModelGRpcRequest处理面部表情和脑电数据
			if functionSwitch.ProcessFacialAndEEGData {
				wg.Add(1)
				go performModelRequest(&wg, pf.appServiceConfig.ModelServer.FacialAndEEGModel.Url, imageValue, eegValue, pf.appServiceConfig.ModelServer.FacialAndEEGModel.Timeout, FacialAndEEGResultChan, errorChan)
			}

			// 异步调用sendModelGRpcRequest处理眼球追踪数据
			if functionSwitch.ProcessEyeTrackingData {
				wg.Add(1)
				go performModelRequest(&wg, pf.appServiceConfig.ModelServer.EyeTrackingModel.Url, imageValue, "", pf.appServiceConfig.ModelServer.EyeTrackingModel.Timeout, EyeTrackingResultChan, errorChan)
			}

			// 异步调用sendEncryptionGRpcRequest加密图像数据
			if functionSwitch.EncryptImageData {
				wg.Add(1)
				go performEncryptionRequest(&wg, pf.appServiceConfig.EncryptionServer.ImageEncryptionServer.Url, imageValue, pf.appServiceConfig.EncryptionServer.ImageEncryptionServer.Timeout, imageEncryptionResultChan, errorChan)
			}

			// 异步调用sendEncryptionGRpcRequest加密脑电数据
			if functionSwitch.EncryptEEGData {
				wg.Add(1)
				go performEncryptionRequest(&wg, pf.appServiceConfig.EncryptionServer.EEGEncryptionServer.Url, eegValue, pf.appServiceConfig.EncryptionServer.EEGEncryptionServer.Timeout, eegEncryptionResultChan, errorChan)
			}

			// 等待四个goroutine完成
			wg.Wait()
			close(errorChan) // 关闭错误通道，表示不再发送任何错误

			// 检查是否有错误返回
			for err := range errorChan {
				if err != nil {
					return false, fmt.Errorf("error occurred during RPC calls: %v", err)
				}
			}

			// 获取结果并进行下一步处理
			if functionSwitch.EncryptImageData {
				imageValue = <-imageEncryptionResultChan
			}
			if functionSwitch.EncryptEEGData {
				eegValue = <-eegEncryptionResultChan
			}
			if functionSwitch.ProcessFacialAndEEGData {
				facial_eeg_model_result = <-FacialAndEEGResultChan
				lc.Infof("data seq : %s, studend ID : %s, facial_eeg_collect_timestamp : %s, got response from rpc server : %s", seqValue, studentIDValue, facial_eeg_collect_timestamp, facial_eeg_model_result)
			}
			if functionSwitch.ProcessEyeTrackingData {
				eye_tracking_mode_result = <-EyeTrackingResultChan
				lc.Infof("data seq : %s, studend ID : %s, eye_tracking_collect_timestamp : %s, got response from rpc server : %s", seqValue, studentIDValue, eye_tracking_collect_timestamp, eye_tracking_mode_result)
			}
		}

		if pf.appServiceConfig.FunctionSwitch.SaveDataToCloud {
			// 存储面部表情、脑电数据及模型处理结果
			studentFacialEEGFeature := config.StudentFacialEEGFeature{
				StudentID:                 studentIDValue,
				TaskID:                    pointerTaskID,
				FacialEegCollectTimestamp: facial_eeg_collect_timestamp,
				FacialExpression:          []byte(imageValue),
				EegData:                   []byte(eegValue),
				FacialEegModelResult:      strings.TrimRight(facial_eeg_model_result, "\n"),
			}
			if err := sendDataToCloud(pf.appServiceConfig.CloudServer, studentFacialEEGFeature); err != nil {
				return false, fmt.Errorf("error occurred during sending http request to cloud : %v", err)
			}

			// 存储眼球追踪数据以及模型处理结果
			studentEyeTrackingFeature := config.StudentEyeTrackingFeature{
				StudentID:                   studentIDValue,
				TaskID:                      pointerTaskID,
				EyeTrackingCollectTimeStamp: eye_tracking_collect_timestamp,
				EyeTrackingData:             []byte(imageValue),
				EyeTrackingModelResult:      strings.TrimRight(eye_tracking_mode_result, "\n"),
			}
			if err := sendDataToCloud(pf.appServiceConfig.CloudServer, studentEyeTrackingFeature); err != nil {
				return false, fmt.Errorf("error occurred during sending http request to cloud : %v", err)
			}
		}

		return true, nil
	} else if resourcename == "KeyboardAndMouse" {
		stringValues, err := parseStringValue(result["seq"], result["student_id"], result["keyboard_mouse_collect_timestamp"], result["keyboard_mouse_data"])
		if err != nil {
			return false, err
		}
		seqValue, studentIDValue, keyboard_mouse_collect_timestamp, keyboardMouseValue := stringValues[0], stringValues[1], stringValues[2], stringValues[3]

		var keyboard_mouse_model_result string
		if pf.appServiceConfig.FunctionSwitch.ProcessKeyboardAndMouseData {
			var wg sync.WaitGroup
			KeyboardAndMouseResultChan := make(chan string, 1)
			errorChan := make(chan error, 1)

			// 异步调用sendModelGRpcRequest处理键鼠数据
			wg.Add(1)
			go performModelRequest(&wg, pf.appServiceConfig.ModelServer.KeyBoardAndMouseModel.Url, keyboardMouseValue, "", pf.appServiceConfig.ModelServer.KeyBoardAndMouseModel.Timeout, KeyboardAndMouseResultChan, errorChan)

			// 等待goroutine完成
			wg.Wait()
			close(errorChan) // 关闭错误通道，表示不再发送任何错误

			// 检查是否有错误返回
			for err := range errorChan {
				if err != nil {
					return false, fmt.Errorf("error occurred during RPC calls: %v", err)
				}
			}

			// 获取结果并进行下一步处理
			keyboard_mouse_model_result = <-KeyboardAndMouseResultChan
			lc.Infof("data seq : %s, studend ID : %s, keyboard_mouse_collect_timestamp : %s, got response from rpc server : %s", seqValue, studentIDValue, keyboard_mouse_collect_timestamp, keyboard_mouse_model_result)
		}

		if pf.appServiceConfig.FunctionSwitch.SaveDataToCloud {
			// 存储键鼠数据以及模型处理结果
			studentKeyboardMouseFeature := config.StudentKeyboardMouseFeature{
				StudentID:                     studentIDValue,
				TaskID:                        pointerTaskID,
				KeyboardMouseCollectTimestamp: keyboard_mouse_collect_timestamp,
				KeyboardMouseData:             []byte(keyboardMouseValue),
				KeyboardMouseModelResult:      strings.TrimRight(keyboard_mouse_model_result, "\n"),
			}
			if err := sendDataToCloud(pf.appServiceConfig.CloudServer, studentKeyboardMouseFeature); err != nil {
				return false, fmt.Errorf("error occurred during sending http request to cloud : %v", err)
			}
		}

		return true, nil
	}

	return false, fmt.Errorf("invalid resourcename, got : %s", resourcename)
}

func sendDataToCloud(cloudServerInfo config.CloudServerInfo, data interface{}) error {
	if data == nil {
		return fmt.Errorf("no data received")
	}

	var err error
	var url string
	var jsonData []byte
	// 将数据转换为 json
	switch t := data.(type) {
	case config.StudentFacialEEGFeature:
		url = cloudServerInfo.DatabaseServer.Url + cloudServerInfo.FacialAndEEGFeaturePath
		jsonData, err = json.Marshal(data.(config.StudentFacialEEGFeature))
		if err != nil {
			return fmt.Errorf("error occurred while encoding StudentFacialEEGFeature data: %v", err)
		}
	case config.StudentEyeTrackingFeature:
		url = cloudServerInfo.DatabaseServer.Url + cloudServerInfo.EyeTrackingFeaturePath
		jsonData, err = json.Marshal(data.(config.StudentEyeTrackingFeature))
		if err != nil {
			return fmt.Errorf("error occurred while encoding StudentEyeTrackingFeature data: %v", err)
		}
	case config.StudentKeyboardMouseFeature:
		url = cloudServerInfo.DatabaseServer.Url + cloudServerInfo.KeyBoardFeaturePath
		jsonData, err = json.Marshal(data.(config.StudentKeyboardMouseFeature))
		if err != nil {
			return fmt.Errorf("error occurred while encoding StudentKeyboardMouseFeature data: %v", err)
		}
	default:
		return fmt.Errorf("unexpected type of data : %T", t)
	}

	// 创建新的 http POST 请求
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error occurred while creating HTTP request: %v", err)
	}

	// 设置 header 信息，告知服务器端发送的是 json 数据格式
	req.Header.Set("Content-Type", "application/json")

	// 创建一个带有超时时间的 context
	httpCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cloudServerInfo.DatabaseServer.Timeout)*time.Second)
	defer cancel()

	// 将创建的 context 附加到 http request 上
	req = req.WithContext(httpCtx)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error occurred while sending HTTP request: %v", err)
	}

	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode == http.StatusOK {
		fmt.Printf("Successfully post facial and eeg feature, http response's StatusCode : %d\n", resp.StatusCode)
		return nil
	} else {
		return fmt.Errorf("failed to post facial and eeg feature, http response's StatusCode : %d", resp.StatusCode)
	}
}

// 封装ModelProcess请求的逻辑
func performModelRequest(wg *sync.WaitGroup, rpcAddress string, imageValue string, eegValue string, timeout int, resultChan chan<- string, errorChan chan<- error) {
	defer wg.Done()
	result, err := sendModelGRpcRequest(rpcAddress, imageValue, eegValue, timeout)
	if err != nil {
		errorChan <- err
		return
	}
	resultChan <- result
}

// 封装加密请求的逻辑
func performEncryptionRequest(wg *sync.WaitGroup, rpcAddress string, dataValue string, timeout int, resultChan chan<- string, errorChan chan<- error) {
	defer wg.Done()
	result, err := sendEncryptionGRpcRequest(rpcAddress, dataValue, timeout)
	if err != nil {
		errorChan <- err
		return
	}
	resultChan <- result
}

// createGrpcConn 创建GRPC连接，并设置超时。
func createGrpcConn(serverAddress string, timeOut int) (*grpc.ClientConn, context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeOut)*time.Second)
	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("cannot connect to RPC server: %v", err)
	}
	return conn, ctx, cancel, nil
}

// sendModelGRpcRequest 发送用于处理模型的RPC请求。
func sendModelGRpcRequest(serverAddress string, featureData_1 string, featureData_2 string, timeOut int) (string, error) {
	conn, ctx, cancel, err := createGrpcConn(serverAddress, timeOut)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer conn.Close()

	var result string
	beginTime := time.Now()

	client := pb_model.NewGRpcServiceClient(conn)
	req := &pb_model.ModelProcessRequest{
		FeatureData_1: featureData_1,
		FeatureData_2: featureData_2,
	}
	resp, err := client.ModelProcess(ctx, req)
	if err != nil {
		return "", fmt.Errorf("fail to get response from rpc server, fail reason: %v", err)
	}
	result = resp.GetResult()

	endTime := time.Since(beginTime)
	fmt.Printf("Function sendModelGRpcRequest run time: %s\n", endTime.String())

	return result, nil
}

// sendEncryptionGRpcRequest 发送加密的RPC请求。
func sendEncryptionGRpcRequest(serverAddress string, data string, timeOut int) (string, error) {
	conn, ctx, cancel, err := createGrpcConn(serverAddress, timeOut)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer conn.Close()

	var result string
	beginTime := time.Now()

	client := pb_encryption.NewGRpcServiceClient(conn)
	req := &pb_encryption.EncryptionProcessRequest{Data: data}
	resp, err := client.EncryptionProcess(ctx, req)
	if err != nil {
		return "", fmt.Errorf("fail to get response from rpc server, fail reason: %v", err)
	}
	result = resp.GetResult()

	endTime := time.Since(beginTime)
	fmt.Printf("Function sendEncryptionGRpcRequest run time: %s\n", endTime.String())

	return result, nil
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
