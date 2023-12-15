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

func Newpipelinefunction(rpcServer config.RemoteServerInfo, encryptionServer config.RemoteServerInfo, cloudServer config.RemoteServerInfo) *pipelinefunction {
	return &pipelinefunction{
		rpcServerInfo:        rpcServer,
		encryptionServerInfo: encryptionServer,
		cloudServerInfo:      cloudServer,
	}
}

type pipelinefunction struct {
	rpcServerInfo        config.RemoteServerInfo
	encryptionServerInfo config.RemoteServerInfo
	cloudServerInfo      config.RemoteServerInfo
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

func (pf *pipelinefunction) ModelProcess(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("ModelProcess called in pipeline '%s'", ctx.PipelineId())

	if data == nil {
		return false, fmt.Errorf("function ModelProcess in pipeline '%s': No Data Received", ctx.PipelineId())
	}
	event, ok := data.(dtos.Event)
	if !ok {
		return false, fmt.Errorf("function ModelProcess in pipeline '%s', type received is not an Event", ctx.PipelineId())
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

	if resourcename == "FacialAndEEG" {
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

		stringValues, err := parseStringValue(result["seq"], result["student_id"], result["facial_eeg_collect_timestamp"], result["image_data"], result["eeg_data"])
		if err != nil {
			return false, err
		}
		seqValue, studentIDValue, facial_eeg_collect_timestamp, imageValue, eegValue := stringValues[0], stringValues[1], stringValues[2], stringValues[3], stringValues[4]

		var wg sync.WaitGroup
		modelResultChan := make(chan string, 1)
		encryptionResultChan := make(chan string, 1)
		errorChan := make(chan error, 2)

		wg.Add(2)

		// 异步调用 sendModelGRpcRequest
		modelRpcAddr := pf.rpcServerInfo.FacialAndEEGUrl
		go func() {
			defer wg.Done()
			result, err := sendModelGRpcRequest(resourcename, modelRpcAddr, imageValue, eegValue, pf.rpcServerInfo.Timeout)
			if err != nil {
				errorChan <- err
				return
			}
			modelResultChan <- result
		}()

		// 异步调用 sendEncryptionGRpcRequest
		encryptionRpcAddr := pf.encryptionServerInfo.FacialAndEEGUrl
		go func() {
			defer wg.Done()
			result, err := sendEncryptionGRpcRequest("image", encryptionRpcAddr, imageValue, pf.encryptionServerInfo.Timeout)
			if err != nil {
				errorChan <- err
				return
			}
			encryptionResultChan <- result
		}()

		// 等待两个goroutine完成
		wg.Wait()
		close(errorChan) // 关闭错误通道，表示不再发送任何错误

		// 检查是否有错误返回
		for err := range errorChan {
			if err != nil {
				return false, fmt.Errorf("error occurred during RPC calls: %v", err)
			}
		}

		// 获取结果并进行下一步处理
		facial_eeg_model_result := <-modelResultChan
		encryptionResult := <-encryptionResultChan
		lc.Infof("data seq : %s, studend ID : %s, facial_eeg_collect_timestamp : %s, got response from rpc server : %s", seqValue, studentIDValue, facial_eeg_collect_timestamp, facial_eeg_model_result)

		studentFacialEEGFeature := config.StudentFacialEEGFeature{
			StudentID:                 studentIDValue,
			TaskID:                    pointerTaskID,
			FacialEegCollectTimestamp: facial_eeg_collect_timestamp,
			FacialExpression:          []byte(encryptionResult),
			EegData:                   []byte(eegValue),
			FacialEegModelResult:      strings.TrimRight(facial_eeg_model_result, "\n"),
		}
		return true, studentFacialEEGFeature
	} else if resourcename == "EyeAndKm" {
		var pointerTaskID *int = nil
		taskID, ok := result["task_id"].(int)
		if ok {
			pointerTaskID = &taskID
		}

		stringValues, err := parseStringValue(result["student_id"], result["eye_km_collect_timestamp"], result["eye_tracking_data"], result["keyboadr_mouse_data"])
		if err != nil {
			return false, err
		}
		studentIDValue, eye_km_collect_timestamp, eyeValue, kmValue := stringValues[0], stringValues[1], stringValues[2], stringValues[3]

		// initialize rpc info and send
		rpcAddr := pf.rpcServerInfo.EyeAndKmUrl
		eye_km_model_result, err := sendModelGRpcRequest(resourcename, rpcAddr, eyeValue, kmValue, pf.rpcServerInfo.Timeout)
		if err != nil {
			return false, err
		}

		lc.Infof("studend ID : %s, eye_km_collect_timestamp : %s, got response from rpc server : %s", studentIDValue, eye_km_collect_timestamp, eye_km_model_result)

		studentEyeKmFeature := config.StudentEyeKmFeature{
			StudentID:             studentIDValue,
			TaskID:                pointerTaskID,
			EyeKmCollectTimestamp: eye_km_collect_timestamp,
			EyeTrackingData:       []byte(eyeValue),
			KeyboadrMouseData:     []byte(kmValue),
			EyeKmModelResult:      strings.TrimRight(eye_km_model_result, "\n"),
		}
		return true, studentEyeKmFeature
	}

	return false, fmt.Errorf("invalid resourcename, got : %s", resourcename)
}

func (pf *pipelinefunction) SendEventToCloud(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("SendEventToCloud called in pipeline '%s'", ctx.PipelineId())

	if data == nil {
		return false, fmt.Errorf("function SendEventToCloud in pipeline '%s': No Data Received", ctx.PipelineId())
	}

	var err error
	var url string
	var jsonData []byte
	// 将数据转换为 json
	switch t := data.(type) {
	case config.StudentFacialEEGFeature:
		url = pf.cloudServerInfo.FacialAndEEGUrl
		jsonData, err = json.Marshal(data.(config.StudentFacialEEGFeature))
	case config.StudentEyeKmFeature:
		url = pf.cloudServerInfo.EyeAndKmUrl
		jsonData, err = json.Marshal(data.(config.StudentEyeKmFeature))
	default:
		return false, fmt.Errorf("unexpected type of data : %T", t)
	}

	if err != nil {
		return false, fmt.Errorf("error occurred while encoding studentEyeKmFeature data: %v", err)
	}

	// 创建新的 http POST 请求
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return false, fmt.Errorf("error occurred while creating HTTP request: %v", err)
	}

	// 设置 header 信息，告知服务器端发送的是 json 数据格式
	req.Header.Set("Content-Type", "application/json")

	// 创建一个带有超时时间的 context
	httpCtx, cancel := context.WithTimeout(context.Background(), time.Duration(pf.cloudServerInfo.Timeout)*time.Second)
	defer cancel()

	// 将创建的 context 附加到 http request 上
	req = req.WithContext(httpCtx)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("error occurred while sending HTTP request: %v", err)
	}

	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode == http.StatusOK {
		fmt.Printf("Successfully post facial and eeg feature, http response's StatusCode : %d\n", resp.StatusCode)
		return true, nil
	} else {
		return false, fmt.Errorf("failed to post facial and eeg feature, http response's StatusCode : %d", resp.StatusCode)
	}
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
func sendModelGRpcRequest(resourceName string, serverAddress string, featureData string, eegData string, timeOut int) (string, error) {
	conn, ctx, cancel, err := createGrpcConn(serverAddress, timeOut)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer conn.Close()

	var result string
	beginTime := time.Now()

	if resourceName == "FacialAndEEG" {
		client := pb_model.NewGRpcServiceClient(conn)
		req := &pb_model.ModelProcessRequest{
			ImageData: featureData,
			EegData:   eegData,
		}
		resp, err := client.ModelProcess(ctx, req)
		if err != nil {
			return "", fmt.Errorf("fail to get response from rpc server, fail reason: %v", err)
		}
		result = resp.GetResult()
	} else if resourceName == "EyeAndKm" {
		client := pb_model.NewGRpcServiceClient(conn)
		req := &pb_model.ModelProcessRequest{
			ImageData: featureData,
			EegData:   eegData,
		}
		resp, err := client.ModelProcess(ctx, req)
		if err != nil {
			return "", fmt.Errorf("fail to get response from rpc server, fail reason: %v", err)
		}
		result = resp.GetResult()
	}

	endTime := time.Since(beginTime)
	fmt.Printf("Function sendModelGRpcRequest run time: %s\n", endTime.String())

	return result, nil
}

// sendEncryptionGRpcRequest 发送加密的RPC请求。
func sendEncryptionGRpcRequest(resourceName string, serverAddress string, data string, timeOut int) (string, error) {
	conn, ctx, cancel, err := createGrpcConn(serverAddress, timeOut)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer conn.Close()

	var result string
	beginTime := time.Now()

	if resourceName == "image" {
		client := pb_encryption.NewGRpcServiceClient(conn)
		req := &pb_encryption.EncryptionProcessRequest{Data: data}
		resp, err := client.EncryptionProcess(ctx, req)
		if err != nil {
			return "", fmt.Errorf("fail to get response from rpc server, fail reason: %v", err)
		}
		result = resp.GetResult()
	}

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
