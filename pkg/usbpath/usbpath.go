package usbpath

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Регулярка для извлечения USB цепочки: 3-1.6.6 или 3-1.2
var usbPathRegex = regexp.MustCompile(`(\d+-\d+(?:\.\d+)+)`)

// Регулярка для извлечения пары chip.port: например 6.6 из 3-1.6.6
var chipPortRegex = regexp.MustCompile(`\.(\d+)\.(\d+)`)

// HardwareHub49Mapping - заводская карта трассировки портов 49-портового хаба
var HardwareHub49Mapping = map[string]int{
	"5.7": 1,
	"5.6": 2,
	"5.5": 3,
	"5.1": 4,
	"5.2": 5,
	"5.3": 6,
	"5.4": 7,

	"6.7": 8,
	"6.6": 9,
	"6.5": 10,
	"6.1": 11,
	"6.2": 12,
	"6.3": 13,
	"6.4": 14,

	"7.7": 15,
	"7.6": 16,
	"7.5": 17,
	"7.1": 18,
	"7.2": 19,
	"7.3": 20,
	"7.4": 21,

	"4.7": 22,
	"4.6": 23,
	"4.5": 24,
	"4.1": 25,
	"4.2": 26,
	"4.3": 27,
	"4.4": 28,

	"3.7": 29,
	"3.6": 30,
	"3.5": 31,
	"3.1": 32,
	"3.2": 33,
	"3.3": 34,
	"3.4": 35,

	"2.7": 36,
	"2.6": 37,
	"2.5": 38,
	"2.1": 39,
	"2.2": 40,
	"2.3": 41,
	"2.4": 42,

	"1.7": 43,
	"1.6": 44,
	"1.5": 45,
	"1.1": 46,
	"1.2": 47,
	"1.3": 48,
	"1.4": 49,
}

// GetUSBPath возвращает физический путь подключения устройства на шине USB (например: "3-1.6.6")
func GetUSBPath(portName string) string {
	devName := strings.TrimPrefix(portName, "/dev/")
	sysPath := filepath.Join("/sys/class/tty", devName)

	realPath, err := filepath.EvalSymlinks(sysPath)
	if err != nil {
		return ""
	}

	// Ищем фрагмент вида /3-1.6.6:1.0/
	parts := strings.Split(realPath, "/")
	for _, p := range parts {
		if strings.Contains(p, ":") {
			prefix := strings.Split(p, ":")[0]
			if usbPathRegex.MatchString(prefix) {
				return prefix
			}
		}
	}

	matches := usbPathRegex.FindAllString(realPath, -1)
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}

	return ""
}

// Calculate49Slot вычисляет номер разъема на 49-портовом хабе (1..49)
func Calculate49Slot(usbPath string, customMapping map[string]int) int {
	// 1. Проверяем кастомную конфигурацию
	if slot, ok := customMapping[usbPath]; ok && slot >= 1 && slot <= 49 {
		return slot
	}

	// 2. Ищем chip.port в HardwareHub49Mapping
	matches := chipPortRegex.FindStringSubmatch(usbPath)
	if len(matches) >= 3 {
		chipKey := fmt.Sprintf("%s.%s", matches[1], matches[2])
		if slot, ok := HardwareHub49Mapping[chipKey]; ok {
			return slot
		}

		chipIdx, _ := strconv.Atoi(matches[1])
		portIdx, _ := strconv.Atoi(matches[2])
		if chipIdx >= 1 && chipIdx <= 7 && portIdx >= 1 && portIdx <= 7 {
			return (chipIdx-1)*7 + portIdx
		}
	}

	return 0
}
