/// One acknowledged delivery. The target finished successfully and reports a
/// result with an output the platform must process.
module delivery_reports {
  const READ_PERSISTED_RESULT: bool

  type Report = AwaitingReport | Committed | Signaled | SignalFailed
  type Workflow = AwaitingCompletion | ProcessingOutputs | Completed
  type State = { report: Report, workflow: Workflow, retried: bool }
  var state: State

  action init = state' = { report: AwaitingReport, workflow: AwaitingCompletion, retried: false }

  // DeliveryReportService.ReportResult commits the terminal state before
  // calling SignalFulfillmentEvent. The proposed variant saves the full
  // generation-scoped result (including outputs) in that same transaction.
  action commitReport = all {
    state.report == AwaitingReport,
    state' = state.with("report", Committed),
  }

  action sendCompletionSignal = all {
    state.report == Committed,
    state' = state.with("report", Signaled),
  }

  action failCompletionSignal = all {
    state.report == Committed,
    state' = state.with("report", SignalFailed),
  }

  // TransitionTo rejects an already terminal delivery. ReportResult returns
  // nil immediately, without repeating the signal or changing the result.
  action retryReport = all {
    state.report == SignalFailed, not(state.retried),
    state' = state.with("retried", true),
  }

  // dispatchAndAwait uses AwaitSignal after the acknowledgement. In the
  // proposed variant, a durable timeout also reloads the committed result.
  action observeCompletion = all {
    state.workflow == AwaitingCompletion,
    state.report == Signaled or (READ_PERSISTED_RESULT and state.report != AwaitingReport),
    state' = state.with("workflow", ProcessingOutputs),
  }

  action processOutputs = all {
    state.workflow == ProcessingOutputs,
    state' = state.with("workflow", Completed),
  }

  val completed = state.workflow == Completed
  val strandedCompletion = state.report == SignalFailed and state.retried and
    state.workflow == AwaitingCompletion and not(READ_PERSISTED_RESULT)

  action reportStep = any { commitReport, sendCompletionSignal, failCompletionSignal, retryReport }
  action workflowStep = any { observeCompletion, processOutputs }
  action step = any { reportStep, workflowStep, state' = state }

  temporal eventualProgress = (reportStep.weakFair(state) and workflowStep.weakFair(state)) implies
    always(eventually(completed))
}

module current_delivery_reports {
  import delivery_reports(READ_PERSISTED_RESULT = false).*

  run lost_completion_signal_strands_acknowledged_delivery = init
    .then(commitReport).then(failCompletionSignal).then(retryReport)
    .expect(strandedCompletion)
}

module persisted_delivery_reports {
  import delivery_reports(READ_PERSISTED_RESULT = true).*

  run timeout_recovers_complete_result_and_outputs = init
    .then(commitReport).then(failCompletionSignal).then(retryReport)
    .then(observeCompletion).then(processOutputs)
    .expect(completed)
}
