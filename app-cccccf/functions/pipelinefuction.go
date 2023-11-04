package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/edgexfoundry/app-functions-sdk-go/v2/pkg/interfaces"

	"github.com/edgexfoundry/go-mod-core-contracts/v2/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/dtos"

	"github.com/edgexfoundry/app-cccccf/config"
	pb "github.com/edgexfoundry/app-cccccf/protobuf"
)

func Newpipelinefunction(rpcServer config.RemoteServerInfo, cloudServer config.RemoteServerInfo) *pipelinefunction {
	return &pipelinefunction{
		rpcServerInfo:   rpcServer,
		cloudServerInfo: cloudServer,
	}
}

type pipelinefunction struct {
	rpcServerInfo   config.RemoteServerInfo
	cloudServerInfo config.RemoteServerInfo
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
		// initialize rpc info and send
		rpcAddr := pf.rpcServerInfo.FacialAndEEGUrl
		stringValues, err := pf.parseStringValue(result["student_id"], result["class_id"], result["facial_eeg_collect_timestamp"], result["image_data"], result["eeg_data"])
		if err != nil {
			return false, err
		}
		studentIDValue, classIDValue, facial_eeg_collect_timestamp, imageValue, eegValue := stringValues[0], stringValues[1], stringValues[2], stringValues[3], stringValues[4]

		facial_eeg_model_result, err := SendGRpcRequest(resourcename, rpcAddr, imageValue, eegValue, pf.rpcServerInfo.Timeout)
		if err != nil {
			return false, err
		}

		lc.Infof("studend ID : %s, class ID : %s, facial_eeg_collect_timestamp : %s, got response from rpc server : %s", studentIDValue, classIDValue, facial_eeg_collect_timestamp, facial_eeg_model_result)

		studentFacialEEGFeature := config.StudentFacialEEGFeature{
			StudentID:                 studentIDValue,
			ClassID:                   classIDValue,
			FacialEegCollectTimestamp: facial_eeg_collect_timestamp,
			FacialExpression:          []byte(imageValue),
			EegData:                   []byte(eegValue),
			FacialEegModelResult:      strings.TrimRight(facial_eeg_model_result, "\n"),
		}
		return true, studentFacialEEGFeature
	} else if resourcename == "EyeAndKm" {
		// initialize rpc info and send
		rpcAddr := pf.rpcServerInfo.EyeAndKmUrl
		stringValues, err := pf.parseStringValue(result["student_id"], result["class_id"], result["eye_km_collect_timestamp"], result["eye_tracking_data"], result["keyboadr_mouse_data"])
		if err != nil {
			return false, err
		}
		studentIDValue, classIDValue, eye_km_collect_timestamp, eyeValue, kmValue := stringValues[0], stringValues[1], stringValues[2], stringValues[3], stringValues[4]

		eye_km_model_result, err := SendGRpcRequest(resourcename, rpcAddr, eyeValue, kmValue, pf.rpcServerInfo.Timeout)
		if err != nil {
			return false, err
		}

		lc.Infof("studend ID : %s, class ID : %s, eye_km_collect_timestamp : %s, got response from rpc server : %s", studentIDValue, classIDValue, eye_km_collect_timestamp, eye_km_model_result)

		studentEyeKmFeature := config.StudentEyeKmFeature{
			StudentID:             studentIDValue,
			ClassID:               classIDValue,
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

func SendGRpcRequest(resourcename string, serverAddress string, featureData string, eegdata string, timeOut int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeOut)*time.Second)
	defer cancel()

	var result string
	beginTime := time.Now() // 开始时间, 计算函数运行时间

	// Set up a connection to the server.
	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return "", fmt.Errorf("cannot connect to RPC server: %v", err)
	}
	defer conn.Close()

	if resourcename == "FacialAndEEG" {
		c := pb.NewGRpcServiceClient(conn)

		// Contact the server, get its response.
		r, err := c.ModelProcess(ctx, &pb.ModelProcessRequest{ImageData: featureData, EegData: eegdata})
		if err != nil {
			return "", fmt.Errorf("fail to get response from rpc server, fail reason: %v", err)
		}

		result = r.GetResult()
	} else { // EyeAndKm Part
		result = ""
	}

	endTime := time.Since(beginTime) // 从开始到当前所消耗的时间
	fmt.Printf("Function SendGRpcRequest run time: %s\n", endTime.String())
	return result, nil
}

func (pf *pipelinefunction) parseStringValue(values ...interface{}) ([]string, error) {
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
