package pdu

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf16"
)

// EncodeGSM7BitHuawei упаковывает USSD строку (например "*111#" или "*100#") в 7-bit Packed Hex
func EncodeGSM7BitHuawei(src string) string {
	bytes := []byte(src)
	var result []byte
	var shift int
	var carry byte

	for i := 0; i < len(bytes); i++ {
		b := bytes[i] & 0x7F
		shift = i % 8
		if shift == 0 {
			carry = b
		} else {
			ch := (b << (8 - shift)) | carry
			result = append(result, ch)
			carry = b >> shift
		}
	}
	if shift != 7 {
		result = append(result, carry)
	}

	return strings.ToUpper(hex.EncodeToString(result))
}

// DecodeGSM7Bit распаковывает 7-битный упакованный Hex от оператора
func DecodeGSM7Bit(hexStr string) string {
	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return hexStr
	}

	var result []byte
	var shift int
	var carry byte

	for i := 0; i < len(bytes); i++ {
		b := bytes[i]
		shift = i % 7

		ch := ((b << shift) | carry) & 0x7F
		result = append(result, ch)
		carry = b >> (7 - shift)

		if shift == 6 {
			result = append(result, carry&0x7F)
			carry = 0
		}
	}

	return string(result)
}

// DecodeUCS2Hex декодирует строку UCS2 Hex в UTF-8
func DecodeUCS2Hex(hexStr string) (string, error) {
	hexStr = strings.TrimSpace(hexStr)
	hexStr = strings.Trim(hexStr, "\"")
	if len(hexStr)%4 != 0 {
		return hexStr, fmt.Errorf("invalid ucs2 hex length: %d", len(hexStr))
	}

	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return hexStr, err
	}

	u16s := make([]uint16, len(bytes)/2)
	for i := 0; i < len(bytes); i += 2 {
		u16s[i/2] = uint16(bytes[i])<<8 | uint16(bytes[i+1])
	}

	runes := utf16.Decode(u16s)
	return string(runes), nil
}

// DecodeUSSDResponse автоматически распаковывает +CUSD: <m>,"<hex>",<dcs>
func DecodeUSSDResponse(raw string) string {
	raw = strings.TrimSpace(raw)

	start := strings.Index(raw, "\"")
	if start == -1 {
		return raw
	}
	end := strings.LastIndex(raw, "\"")
	if end <= start {
		return raw
	}

	content := raw[start+1 : end]

	if !isHexOnly(content) {
		return content
	}

	// 1. Проверяем, не UCS2 ли это (русские буквы обычно начинаются с 04xx)
	if len(content)%4 == 0 && len(content) >= 4 {
		// Если первые 2 байта похожи на 04 (кириллица) или 00 (ascii)
		if (content[:2] == "04" || content[:2] == "00") {
			if decoded, err := DecodeUCS2Hex(content); err == nil && len(decoded) > 0 {
				return decoded
			}
		}
	}

	// 2. Распаковываем как GSM 7-bit
	decoded7bit := DecodeGSM7Bit(content)
	if len(decoded7bit) > 0 {
		return decoded7bit
	}

	return content
}

func isHexOnly(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
