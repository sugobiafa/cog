import sys
from datetime import datetime, timezone
from fastapi.encoders import jsonable_encoder
from multiprocessing import Event
from multiprocessing.pool import ThreadPool, AsyncResult
from typing import Any, Callable, Dict, Optional, Tuple

from .. import schema
from .eventtypes import Done, Heartbeat, Log, PredictionOutput, PredictionOutputType
from .webhook import webhook_caller, webhook_caller_filtered
from .worker import Worker


class PredictionRunner:
    def __init__(self, predictor_ref: str):
        self.current_prediction_id = None
        self._thread = None
        self._threadpool = ThreadPool(processes=1)

        self._result: Optional[AsyncResult] = None
        self._last_result = None

        self._worker = Worker(predictor_ref=predictor_ref)
        self._should_cancel = Event()

    def setup(self) -> Tuple[schema.Status, str]:
        _logs = []
        _status = None

        try:
            for event in self._worker.setup():
                if isinstance(event, Log):
                    _logs.append(event.message)
                elif isinstance(event, Done):
                    _status = (
                        schema.Status.FAILED if event.error else schema.Status.SUCCEEDED
                    )
        except Exception:
            _status = schema.Status.FAILED

        assert _status is not None, "must receive done event from setup"

        return (_status, "".join(_logs))

    # TODO: Make the return type AsyncResult[schema.PredictionResponse] when we
    # no longer have to support Python 3.8
    def predict(self, prediction: schema.PredictionRequest) -> AsyncResult:
        # It's the caller's responsibility to not call us if we're busy.
        assert not self.is_busy()

        self._should_cancel.clear()
        event_handler = create_event_handler(prediction)

        def cleanup(_):
            if hasattr(prediction.input, "cleanup"):
                prediction.input.cleanup()

        self._result = self._threadpool.apply_async(
            func=predict,
            args=(self._worker, prediction, self._should_cancel, event_handler),
            callback=cleanup,
            error_callback=cleanup,
        )

        self.current_prediction_id = prediction.id
        return (event_handler.response, self._result)

    def is_busy(self) -> bool:
        if self._result is None:
            return False

        if not self._result.ready():
            return True

        self._last_result = self._result.get()
        self._result = None
        return False

    def shutdown(self) -> None:
        self._threadpool.terminate()
        self._threadpool.join()
        self._worker.terminate()

    def cancel(self) -> None:
        self._should_cancel.set()


def create_event_handler(prediction):
    response = schema.PredictionResponse(**prediction.dict())

    webhook = prediction.webhook
    events_filter = (
        prediction.webhook_events_filter or schema.WebhookEvent.default_events()
    )

    webhook_sender = None
    if webhook is not None:
        webhook_sender = webhook_caller_filtered(webhook, events_filter)

    event_handler = PredictionEventHandler(response, webhook_sender=webhook_sender)

    return event_handler


class PredictionEventHandler:
    def __init__(
        self, p: schema.PredictionResponse, webhook_sender: Optional[Callable] = None
    ):
        self.p = p
        self.p.status = schema.Status.PROCESSING
        self.p.output = None
        self.p.logs = ""
        self.p.started_at = datetime.now(tz=timezone.utc)

        self._webhook_sender = webhook_sender

        self._send_webhook(schema.WebhookEvent.START)

    @property
    def response(self):
        return self.p

    def set_output(self, output: Any) -> None:
        assert self.p.output is None, "Predictor unexpectedly returned multiple outputs"
        self.p.output = output
        # We don't send a webhook for compatibility with the behaviour of
        # redis_queue. In future we can consider whether it makes sense to send
        # one here.

    def append_output(self, output: Any) -> None:
        assert isinstance(
            self.p.output, list
        ), "Cannot append output before setting output"
        self.p.output.append(output)
        self._send_webhook(schema.WebhookEvent.OUTPUT)

    def append_logs(self, logs: str) -> None:
        assert self.p.logs is not None
        self.p.logs += logs
        self._send_webhook(schema.WebhookEvent.LOGS)

    def succeeded(self) -> None:
        self.p.status = schema.Status.SUCCEEDED
        self._set_completed_at()
        # These have been set already: this is to convince the typechecker of
        # that...
        assert self.p.completed_at is not None
        assert self.p.started_at is not None
        self.p.metrics = {
            "predict_time": (self.p.completed_at - self.p.started_at).total_seconds()
        }
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def failed(self, error: str) -> None:
        self.p.status = schema.Status.FAILED
        self.p.error = error
        self._set_completed_at()
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def canceled(self) -> None:
        self.p.status = schema.Status.CANCELED
        self._set_completed_at()
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def _set_completed_at(self) -> None:
        self.p.completed_at = datetime.now(tz=timezone.utc)

    def _send_webhook(self, event: schema.WebhookEvent) -> None:
        if self._webhook_sender is not None:
            dict_response = jsonable_encoder(self.response.dict())
            self._webhook_sender(dict_response, event)


def predict(
    worker: Worker,
    request: schema.PredictionRequest,
    should_cancel: Event,
    event_handler: PredictionEventHandler,
) -> schema.PredictionResponse:
    initial_prediction = request.dict()

    output_type = None
    for event in worker.predict(initial_prediction["input"], poll=0.1):
        if should_cancel.is_set():
            worker.cancel()
            should_cancel.clear()

        if isinstance(event, Heartbeat):
            # Heartbeat events exist solely to ensure that we have a
            # regular opportunity to check for cancelation and
            # timeouts.
            #
            # We don't need to do anything with them.
            pass

        elif isinstance(event, Log):
            event_handler.append_logs(event.message)

        elif isinstance(event, PredictionOutputType):
            if output_type is not None:
                event_handler.failed(error="Predictor returned unexpected output")
                break

            output_type = event
            if output_type.multi:
                event_handler.set_output([])
        elif isinstance(event, PredictionOutput):
            if output_type is None:
                event_handler.failed(error="Predictor returned unexpected output")
                break

            # TODO this should be handled by the arbiter container
            # output = upload_files(event.payload)

            if output_type.multi:
                event_handler.append_output(event.payload)
            else:
                event_handler.set_output(event.payload)

        elif isinstance(event, Done):
            # TODO handle timeouts
            if event.canceled:
                event_handler.canceled()
            elif event.error:
                event_handler.failed(error=str(event.error_detail))
            else:
                event_handler.succeeded()

        else:
            print(f"Received unexpected event from worker: {event}", file=sys.stderr)

    return event_handler.response
