package functions

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"

	gometrics "github.com/rcrowley/go-metrics"

	"github.com/edgexfoundry/app-functions-sdk-go/v2/pkg/interfaces"

	"github.com/edgexfoundry/go-mod-core-contracts/v2/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v2/dtos"
)

const eventsSendToCloudName = "eventsSendToCloud"

func Newpipelinefunction(host string, port int) *pipelinefunction {
	return &pipelinefunction{
		host: host,
		port: port,
	}
}

type pipelinefunction struct {
	// TODO: Remove pipelinefunction metric and implement meaningful metrics if any needed.
	eventsSendToCloud gometrics.Counter
	// TODO: Add properties that the function(s) will need each time one is executed
	result       map[string]interface{}
	resourcename string
	host         string
	port         int
	jsonData     []uint8
}

func (s *pipelinefunction) LogEventDetails(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
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

	lc.Infof("Event received in pipeline '%s': ID=%s, Device=%s, and ReadingCount=%d",
		ctx.PipelineId(),
		event.Id,
		event.DeviceName,
		len(event.Readings))
	for index, reading := range event.Readings {
		switch strings.ToLower(reading.ValueType) {
		case strings.ToLower(common.ValueTypeBinary):
			lc.Infof(
				"Reading #%d received in pipeline '%s' with ID=%s, Resource=%s, ValueType=%s, MediaType=%s and BinaryValue of size=`%d`",
				index+1,
				ctx.PipelineId(),
				reading.Id,
				reading.ResourceName,
				reading.ValueType,
				reading.MediaType,
				len(reading.Value))
		case strings.ToLower(common.ValueTypeObject):
			lc.Infof(
				"Reading #%d received in pipeline '%s' with ID=%s, Resource=%s, ValueType=%s, MediaType=%s and ObjectValue accept",
				index+1,
				ctx.PipelineId(),
				reading.Id,
				reading.ResourceName,
				reading.ValueType,
				reading.MediaType)

		default:
			lc.Infof("Reading #%d received in pipeline '%s' with ID=%s, Resource=%s, ValueType=%s, Value=`%s`",
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

func (s *pipelinefunction) FacialAndEEGModels(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("SendToCloud called in pipeline '%s'", ctx.PipelineId())

	if data == nil {
		return false, fmt.Errorf("function SendToCloud in pipeline '%s': No Data Received", ctx.PipelineId())
	}
	event, ok := data.(dtos.Event)
	if !ok {
		return false, fmt.Errorf("function LogEventDetails in pipeline '%s', type received is not an Event", ctx.PipelineId())
	}

	// //run analysemodel
	// pythonScript := "facailandeegmodels/hello.py"
	// a := "3"
	// b := "4"
	// // 使用 exec.Command 运行 Python 脚本并传递参数
	// cmd := exec.Command("python", pythonScript, a, b)

	// output, err := cmd.Output()
	// if err != nil {
	// 	fmt.Println("Failed to execute Python script:", err)
	// }
	// lc.Infof("Python function returned: %s", strings.TrimRight(string(output), "\n"))

	output := "facial and eeg model successfully compute the result"

	//save result
	for _, reading := range event.Readings {

		//lc.Infof("%s\n", reading.ObjectReading.ObjectValue)
		s.resourcename = reading.ResourceName
		s.result, ok = reading.ObjectReading.ObjectValue.(map[string]interface{})
		if !ok {
			fmt.Println("Failed to convert ObjectReading to map[string]string")
		}
		lc.Infof("success to convert ObjectReading to map[string]interface{}")
	}

	//take out data
	//dataValue := s.result["data"]
	//lc.Infof("%s", dataValue)

	//save analyseresult
	s.result["analyseresult"] = strings.TrimRight(string(output), "\n")
	lc.Infof("%s", s.result["analyseresult"])

	return true, event
}

func (s *pipelinefunction) SendEventToCloud(ctx interfaces.AppFunctionContext, data interface{}) (bool, interface{}) {
	lc := ctx.LoggingClient()
	lc.Debugf("SendToCloud called in pipeline '%s'", ctx.PipelineId())

	lc.Infof("This part is not yet implemented")

	// if data == nil {
	// 	return false, fmt.Errorf("function SendToCloud in pipeline '%s': No Data Received", ctx.PipelineId())
	// }
	// lc.Infof("%s", s.host)
	// lc.Infof("%d", s.port)
	// jsonData, err := json.Marshal(s.result)
	// if err != nil {
	// 	fmt.Println("Error encoding JSON:", err.Error())
	// }
	// lc.Infof("%s", reflect.TypeOf(jsonData))
	// //lc.Infof("%s", jsonData)

	// conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", s.host, s.port))
	// if err != nil {
	// 	fmt.Errorf("fail connected:", err)
	// }
	// defer conn.Close()

	// //strData := fmt.Sprintf("%v", s.jsonData)
	// _, err = conn.Write(jsonData)
	// if err != nil {
	// 	fmt.Errorf("failed send data:", err)
	// }
	// fmt.Println("send data successfully.")

	if s.eventsSendToCloud == nil {
		var err error

		s.eventsSendToCloud = gometrics.NewCounter()
		metricsManger := ctx.MetricsManager()
		if metricsManger != nil {
			err = metricsManger.Register(eventsSendToCloudName, s.eventsSendToCloud, nil)
		} else {
			err = errors.New("metrics manager not available")
		}

		if err != nil {
			lc.Errorf("Unable to register metric %s. Collection will continue, but metric will not be reported: %s", eventsSendToCloudName, err.Error())
		}

	}
	s.eventsSendToCloud.Inc(1)

	return true, nil
}
