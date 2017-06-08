package runner

import (
	"github.com/rcrowley/go-metrics"
	"github.com/streamsets/dataextractor/container/execution"
	"github.com/streamsets/dataextractor/container/util"
	"github.com/streamsets/dataextractor/container/validation"
	"log"
	"time"
)

const (
	INPUT_RECORDS    = ".inputRecords"
	OUTPUT_RECORDS   = ".outputRecords"
	ERROR_RECORDS    = ".errorRecords"
	STAGE_ERRORS     = ".stageErrors"
	BATCH_PROCESSING = ".batchProcessing"
)

type Pipe interface {
	Init() []validation.Issue
	Process(pipeBatch *FullPipeBatch) error
	Destroy()
	IsSource() bool
	IsTarget() bool
}

type StagePipe struct {
	config                 execution.Config
	Stage                  StageRuntime
	InputLanes             []string
	OutputLanes            []string
	EventLanes             []string
	inputRecordsCounter    metrics.Counter
	outputRecordsCounter   metrics.Counter
	errorRecordsCounter    metrics.Counter
	stageErrorsCounter     metrics.Counter
	inputRecordsMeter      metrics.Meter
	outputRecordsMeter     metrics.Meter
	errorRecordsMeter      metrics.Meter
	stageErrorsMeter       metrics.Meter
	inputRecordsHistogram  metrics.Histogram
	outputRecordsHistogram metrics.Histogram
	errorRecordsHistogram  metrics.Histogram
	stageErrorsHistogram   metrics.Histogram
	processingTimer        metrics.Timer
}

func (s *StagePipe) Init() []validation.Issue {
	issues := s.Stage.Init()
	if len(issues) == 0 {
		metricRegistry := s.Stage.stageContext.GetMetrics()
		metricsKey := "stage." + s.Stage.config.InstanceName

		s.inputRecordsCounter = util.CreateCounter(metricRegistry, metricsKey+INPUT_RECORDS)
		s.outputRecordsCounter = util.CreateCounter(metricRegistry, metricsKey+OUTPUT_RECORDS)
		s.errorRecordsCounter = util.CreateCounter(metricRegistry, metricsKey+ERROR_RECORDS)
		s.stageErrorsCounter = util.CreateCounter(metricRegistry, metricsKey+STAGE_ERRORS)

		s.inputRecordsMeter = util.CreateMeter(metricRegistry, metricsKey+INPUT_RECORDS)
		s.outputRecordsMeter = util.CreateMeter(metricRegistry, metricsKey+OUTPUT_RECORDS)
		s.errorRecordsMeter = util.CreateMeter(metricRegistry, metricsKey+ERROR_RECORDS)
		s.stageErrorsMeter = util.CreateMeter(metricRegistry, metricsKey+STAGE_ERRORS)

		s.inputRecordsHistogram = util.CreateHistogram5Min(metricRegistry, metricsKey+INPUT_RECORDS)
		s.outputRecordsHistogram = util.CreateHistogram5Min(metricRegistry, metricsKey+OUTPUT_RECORDS)
		s.errorRecordsHistogram = util.CreateHistogram5Min(metricRegistry, metricsKey+ERROR_RECORDS)
		s.stageErrorsHistogram = util.CreateHistogram5Min(metricRegistry, metricsKey+STAGE_ERRORS)

		s.processingTimer = util.CreateTimer(metricRegistry, metricsKey+BATCH_PROCESSING)
	}

	return issues
}

func (s *StagePipe) Process(pipeBatch *FullPipeBatch) error {
	log.Println("[DEBUG] Processing Stage - " + s.Stage.config.InstanceName)
	start := time.Now()
	batchMaker := pipeBatch.StartStage(*s)
	batchImpl := pipeBatch.GetBatch(*s)
	newOffset, err := s.Stage.Execute(pipeBatch.GetPreviousOffset(), s.config.MaxBatchSize, batchImpl, batchMaker)

	if err != nil {
		return err
	}

	if s.IsSource() {
		pipeBatch.SetNewOffset(newOffset)
	}
	pipeBatch.CompleteStage(batchMaker)

	// Update metric registry
	s.processingTimer.UpdateSince(start)

	inputRecordsCount := int64(len(batchImpl.records))
	s.inputRecordsCounter.Inc(inputRecordsCount)
	s.inputRecordsMeter.Mark(inputRecordsCount)
	s.inputRecordsHistogram.Update(inputRecordsCount)

	outputRecordsCount := int64(len(batchMaker.stageOutput))
	s.outputRecordsCounter.Inc(outputRecordsCount)
	s.outputRecordsMeter.Mark(outputRecordsCount)
	s.outputRecordsHistogram.Update(outputRecordsCount)

	instanceName := s.Stage.config.InstanceName
	errorSink := pipeBatch.GetErrorSink()

	stageErrorRecordsCount := int64(len(errorSink.GetStageErrorRecords(instanceName)))
	stageErrorMessagesCount := int64(len(errorSink.GetStageErrorMessages(instanceName)))

	s.errorRecordsCounter.Inc(stageErrorRecordsCount)
	s.errorRecordsMeter.Mark(stageErrorRecordsCount)
	s.errorRecordsHistogram.Update(stageErrorRecordsCount)
	s.stageErrorsCounter.Inc(stageErrorMessagesCount)
	s.stageErrorsMeter.Mark(stageErrorMessagesCount)
	s.stageErrorsHistogram.Update(stageErrorMessagesCount)

	return nil
}

func (s *StagePipe) Destroy() {
	s.Stage.Destroy()
}

func (s *StagePipe) IsSource() bool {
	return len(s.OutputLanes) > 0
}

func (s *StagePipe) IsTarget() bool {
	return len(s.OutputLanes) == 0
}

func NewStagePipe(stage StageRuntime, config execution.Config) Pipe {
	stagePipe := &StagePipe{}
	stagePipe.config = config
	stagePipe.Stage = stage
	stagePipe.InputLanes = stage.config.InputLanes
	stagePipe.OutputLanes = stage.config.OutputLanes
	stagePipe.EventLanes = stage.config.EventLanes
	return stagePipe
}
