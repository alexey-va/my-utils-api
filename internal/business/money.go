package business

import (
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var decimalQuantity = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,6})?$`)
var digits = regexp.MustCompile(`^[0-9]+$`)

func lineAmounts(items []LineItem) ([]LineItem, int64, error) {
	if len(items) == 0 || len(items) > 100 {
		return nil, 0, invalid("Укажите от 1 до 100 позиций")
	}
	result := make([]LineItem, len(items))
	var total int64
	for index, item := range items {
		item.Description = strings.TrimSpace(item.Description)
		item.Quantity = strings.TrimSpace(item.Quantity)
		item.Unit = strings.TrimSpace(item.Unit)
		if item.Description == "" || utf8.RuneCountInString(item.Description) > 1000 || utf8.RuneCountInString(item.Unit) > 32 || !decimalQuantity.MatchString(item.Quantity) || item.PriceMinor < 0 || item.PriceMinor > maxMinor {
			return nil, 0, invalid("Неверная позиция счёта")
		}
		quantity, ok := new(big.Rat).SetString(item.Quantity)
		if !ok || quantity.Sign() <= 0 {
			return nil, 0, invalid("Количество должно быть положительным")
		}
		item.Quantity = strings.TrimRight(strings.TrimRight(quantity.FloatString(6), "0"), ".")
		amount := new(big.Rat).Mul(quantity, new(big.Rat).SetInt64(item.PriceMinor))
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(amount.Num(), amount.Denom(), remainder)
		if new(big.Int).Lsh(remainder, 1).Cmp(amount.Denom()) >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		if !quotient.IsInt64() || quotient.Int64() > maxMinor-total {
			return nil, 0, invalid("Сумма счёта слишком велика")
		}
		item.AmountMinor = quotient.Int64()
		total += item.AmountMinor
		result[index] = item
	}
	if total <= 0 {
		return nil, 0, invalid("Сумма счёта должна быть положительной")
	}
	return result, total, nil
}
func periodDates(period string) (string, string, error) {
	date, err := time.Parse("2006-01", period)
	if err != nil || date.Format("2006-01") != period {
		return "", "", invalid("Период должен иметь формат YYYY-MM")
	}
	return date.Format(time.DateOnly), date.AddDate(0, 1, -1).Format(time.DateOnly), nil
}
func validDate(date string) bool {
	parsed, err := time.Parse(time.DateOnly, date)
	return err == nil && parsed.Format(time.DateOnly) == date
}
func validID(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, "/\\\x00\r\n")
}
func validINN(inn string, optional bool) bool {
	return optional && inn == "" || (len(inn) == 10 || len(inn) == 12) && digits.MatchString(inn)
}
