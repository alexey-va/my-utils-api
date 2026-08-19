package httpapi

import (
	"io"
	"net/http"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/telegram"
)

func (a *API) uploadTelegramFile(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, telegram.MaxFileSize+(1<<20))
	if err := request.ParseMultipartForm(telegram.MaxFileSize + (1 << 20)); err != nil {
		writeError(response, http.StatusRequestEntityTooLarge, "File is larger than 20 MB")
		return
	}
	if request.MultipartForm != nil {
		defer request.MultipartForm.RemoveAll()
	}
	file, header, err := request.FormFile("file")
	if err != nil {
		writeError(response, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, telegram.MaxFileSize+1))
	if err != nil {
		writeError(response, http.StatusBadRequest, "Cannot read file")
		return
	}
	if len(data) > telegram.MaxFileSize {
		writeError(response, http.StatusRequestEntityTooLarge, "File is larger than 20 MB")
		return
	}
	result, err := a.telegramFiles.Deliver(
		request.Context(), request.Header.Get("X-Telegram-File-Token"), header.Filename,
		header.Header.Get("Content-Type"), strings.TrimSpace(request.FormValue("caption")), data,
	)
	writeDomainResult(response, result, err)
}
