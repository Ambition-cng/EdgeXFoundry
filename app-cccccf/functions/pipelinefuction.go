package functions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	gometrics "github.com/rcrowley/go-metrics"

	"github.com/edgexfoundry/app-functions-sdk-go/v2/pkg/interfaces"

	"github.com/edgexfoundry/go-mod-core-contracts/v2/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/dtos"

	"github.com/edgexfoundry/app-cccccf/config"
	pb "github.com/edgexfoundry/app-cccccf/protobuf"
)

const eventsSendToCloudName = "eventsSendToCloud"

func Newpipelinefunction(rpcServer config.RemoteServerInfo, cloudServer config.RemoteServerInfo) *pipelinefunction {
	return &pipelinefunction{
		rpcServerInfo:   rpcServer,
		cloudServerInfo: cloudServer,
	}
}

type pipelinefunction struct {
	// TODO: Remove pipelinefunction metric and implement meaningful metrics if any needed.
	eventsSendToCloud gometrics.Counter
	// TODO: Add properties that the function(s) will need each time one is executed
	result          map[string]interface{}
	resourcename    string
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

func (pf *pipelinefunction) FacialAndEEGModels(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("FacialAndEEGModels called in pipeline '%s'", ctx.PipelineId())

	if data == nil {
		return false, fmt.Errorf("function FacialAndEEGModels in pipeline '%s': No Data Received", ctx.PipelineId())
	}
	event, ok := data.(dtos.Event)
	if !ok {
		return false, fmt.Errorf("function FacialAndEEGModels in pipeline '%s', type received is not an Event", ctx.PipelineId())
	}

	//save result
	for _, reading := range event.Readings {
		pf.resourcename = reading.ResourceName
		pf.result, ok = reading.ObjectReading.ObjectValue.(map[string]interface{})
		if !ok {
			fmt.Println("Failed to convert ObjectReading to map[string]string")
		}
		lc.Debugf("success to convert ObjectReading to map[string]interface{}")
	}

	var rpcAddr = fmt.Sprintf("%s:%d", pf.rpcServerInfo.Host, pf.rpcServerInfo.Port)

	stringValues, err := pf.parseStringValue(pf.result["image_data"], pf.result["eeg_data"], pf.result["stu_name"], pf.result["stu_id"])
	if err != nil {
		return false, err
	}
	imageValue, eegValue, stuNameValue, stuIDValue := stringValues[0], stringValues[1], stringValues[2], stringValues[3]

	floatValues, err := pf.parseFloat64Value(pf.result["seq"])
	if err != nil {
		return false, err
	}
	seqValue := floatValues[0]

	output, err := SendGRpcRequest(rpcAddr, imageValue, eegValue, pf.rpcServerInfo.Timeout)
	if err != nil {
		return false, err
	}

	lc.Infof("student name : %s, studend ID : %s, input data seq : %d, got response from rpc server : %s", stuNameValue, stuIDValue, int(seqValue), output)

	//save analyseresult
	pf.result["analyseresult"] = strings.TrimRight(string(output), "\n")

	return true, event
}

func (pf *pipelinefunction) SendEventToCloud(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("SendEventToCloud called in pipeline '%s'", ctx.PipelineId())

	if pf.eventsSendToCloud == nil {
		var err error

		pf.eventsSendToCloud = gometrics.NewCounter()
		metricsManger := ctx.MetricsManager()
		if metricsManger != nil {
			err = metricsManger.Register(eventsSendToCloudName, pf.eventsSendToCloud, nil)
		} else {
			err = errors.New("metrics manager not available")
		}

		if err != nil {
			lc.Errorf("Unable to register metric %pf. Collection will continue, but metric will not be reported: %s", eventsSendToCloudName, err.Error())
		}

	}
	pf.eventsSendToCloud.Inc(1)

	return true, nil
}

func SendGRpcRequest(serverAddress string, imagedata string, eegdata string, timeOut int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeOut)*time.Second)
	defer cancel()

	beginTime := time.Now() // 开始时间, 计算函数运行时间

	// Set up a connection to the server.
	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return "", fmt.Errorf("cannot connect to RPC server: %v", err)
	}
	defer conn.Close()
	c := pb.NewGRpcServiceClient(conn)

	// Contact the server, calculate the execution time and return its response.
	r, err := c.ModelProcess(ctx, &pb.ModelProcessRequest{ImageData: imagedata, EegData: eegdata})
	if err != nil {
		return "", fmt.Errorf("fail to get response from rpc server, fail reason: %v", err)
	}

	endTime := time.Since(beginTime) // 从开始到当前所消耗的时间
	fmt.Printf("Function SendGRpcRequest run time: %s\n", endTime.String())
	return r.GetResult(), nil
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

func (pf *pipelinefunction) parseFloat64Value(values ...interface{}) ([]float64, error) {
	var floats []float64
	for _, value := range values {
		switch v := value.(type) {
		case float64:
			floats = append(floats, v)
		default:
			return nil, fmt.Errorf("value is not float64, data content: %s", value)
		}
	}

	return floats, nil
}
