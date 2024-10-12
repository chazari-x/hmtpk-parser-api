package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"time"

	hmtpk "github.com/chazari-x/hmtpk_parser/v2"
	hmtpkErrors "github.com/chazari-x/hmtpk_parser/v2/errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// API is the handler for the API
type API struct {
	log   *logrus.Logger
	hmtpk *hmtpk.Controller
}

// NewApi creates a new API
func NewApi(redis *redis.Client, logger *logrus.Logger) *API {
	if logger == nil {
		logger = logrus.New()
		logger.SetLevel(logrus.TraceLevel)
		logger.SetReportCaller(true)
		logger.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
			FullTimestamp:   true,
			PadLevelText:    true,
			CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
				return "", fmt.Sprintf(" %s:%d", frame.File, frame.Line)
			},
		})
	}

	return &API{logger, hmtpk.NewController(redis, logger)}
}

// Router returns the router for the API
func (a *API) Router() func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(a.headersMiddleware)

		r.Post("/groups", a.groups)
		r.Post("/teachers", a.teachers)
		r.Post("/schedule", a.schedule)

		r.Post("/announces", a.announces)
	}
}

func (a *API) headersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

type Response struct {
	Message string `json:",omitempty"`
	Error   string `json:",omitempty"`
}

// write writes the response
func write(w http.ResponseWriter, statusCode int, data interface{}) {
	if statusCode != http.StatusOK {
		w.WriteHeader(statusCode)
	}

	if data == nil {
		if statusCode == http.StatusOK {
			data = Response{Message: http.StatusText(statusCode)}
		} else {
			data = Response{Error: http.StatusText(statusCode)}
		}
	}

	_ = json.NewEncoder(w).Encode(data)
}

const (
	timeout        = time.Second * 15
	requestTimeout = time.Millisecond * 200

	ErrorHmtpkNotWorking = "Превышено время ожидания ответа от https://hmtpk.ru"
	ErrorBadRequest      = "Неверный запрос"
	ErrorToken           = "Ошибка токена пользователя"
	ErrorRequestTimeout  = "Превышено количество запросов к ХМТПК API в секунду"
	ErrorAny             = "Произошла ошибка в ХМТПК API"
)

func (a *API) teachers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	options, err := a.hmtpk.GetTeacherOptions(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			write(w, http.StatusInternalServerError, Response{Error: ErrorHmtpkNotWorking})
			return
		} else if errors.Is(err, hmtpkErrors.ErrorBadResponse) {
			write(w, http.StatusInternalServerError, Response{Error: err.Error()})
			return
		}

		a.log.Error(err)

		write(w, http.StatusInternalServerError, Response{Error: ErrorAny})
		return
	}

	write(w, http.StatusOK, options)
}

func (a *API) groups(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	options, err := a.hmtpk.GetGroupOptions(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			write(w, http.StatusInternalServerError, Response{Error: ErrorHmtpkNotWorking})
			return
		} else if errors.Is(err, hmtpkErrors.ErrorBadResponse) {
			write(w, http.StatusInternalServerError, Response{Error: err.Error()})
			return
		}

		a.log.Error(err)

		write(w, http.StatusInternalServerError, Response{Error: ErrorAny})
		return
	}

	write(w, http.StatusOK, options)
}

func (a *API) schedule(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		write(w, http.StatusBadRequest, Response{Error: ErrorBadRequest})
		return
	}

	date := r.URL.Query().Get("date")
	if date != "" {
		if _, err := time.Parse("02.01.2006", date); err != nil {
			write(w, http.StatusBadRequest, Response{Error: ErrorBadRequest})
			return
		}
	} else {
		date = time.Now().Format("02.01.2006")
	}

	group := r.URL.Query().Get("group")
	if group != "" {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		scheduleByGroup, err := a.hmtpk.GetScheduleByGroup(ctx, group, date)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				write(w, http.StatusInternalServerError, Response{Error: ErrorHmtpkNotWorking})
				return
			} else if errors.Is(err, hmtpkErrors.ErrorBadRequest) {
				write(w, http.StatusBadRequest, Response{Error: err.Error()})
				return
			} else if errors.Is(err, hmtpkErrors.ErrorBadResponse) {
				write(w, http.StatusInternalServerError, Response{Error: err.Error()})
				return
			}

			a.log.Error(err)

			write(w, http.StatusInternalServerError, Response{Error: ErrorAny})
			return
		}

		write(w, http.StatusOK, scheduleByGroup)
		return
	}

	teacher := r.URL.Query().Get("teacher")
	if teacher != "" {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		scheduleByTeacher, err := a.hmtpk.GetScheduleByTeacher(ctx, teacher, date)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				write(w, http.StatusInternalServerError, Response{Error: ErrorHmtpkNotWorking})
				return
			} else if errors.Is(err, hmtpkErrors.ErrorBadRequest) {
				write(w, http.StatusBadRequest, Response{Error: err.Error()})
				return
			} else if errors.Is(err, hmtpkErrors.ErrorBadResponse) {
				write(w, http.StatusInternalServerError, Response{Error: err.Error()})
				return
			}

			a.log.Error(err)

			write(w, http.StatusInternalServerError, Response{Error: ErrorAny})
			return
		}

		write(w, http.StatusOK, scheduleByTeacher)
		return
	}

	write(w, http.StatusBadRequest, Response{Error: ErrorBadRequest})
}

func (a *API) announces(w http.ResponseWriter, r *http.Request) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil {
		write(w, http.StatusBadRequest, Response{Error: ErrorBadRequest})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	announces, err := a.hmtpk.GetAnnounces(ctx, page)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			write(w, http.StatusInternalServerError, Response{Error: ErrorHmtpkNotWorking})
			return
		} else if errors.Is(err, hmtpkErrors.ErrorBadResponse) {
			write(w, http.StatusInternalServerError, Response{Error: err.Error()})
			return
		}

		a.log.Error(err)

		write(w, http.StatusInternalServerError, Response{Error: ErrorAny})
		return
	}

	write(w, http.StatusOK, announces)
}
