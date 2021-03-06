package logging

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"
)

// LogLevel defines log levels
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

//Metadata convenience type for setting metadata
type Metadata map[string]interface{}

func (l LogLevel) String() string {
	return [...]string{"DEBUG", "INFO", "WARN", "ERROR"}[l]
}

type writeFn func(string) error

// Logger implements a simple logger with 4 levels
type Logger struct {
	Level     LogLevel
	Timestamp bool
	metadata  map[string]interface{}
	logfile   *os.File
	write     writeFn
}

// NewLogger logger factory
func NewLogger(level LogLevel, target string) (*Logger, error) {
	var logger Logger
	logger.Level = level
	logger.Timestamp = false
	logger.metadata = make(map[string]interface{})

	switch strings.ToLower(target) {
	case "console":
		logger.write = func(message string) error {
			fmt.Print(message)
			return nil
		}
		break
	default:
		var err error
		if logger.logfile == nil {
			logger.logfile, err = os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				return nil, err
			}
		}
		logger.write = func(message string) error {
			_, err := logger.logfile.WriteString(message)
			return err
		}
	}

	return &logger, nil
}

// Destroy cleanup resources
func (l *Logger) Destroy() error {
	if l.logfile != nil {
		return l.logfile.Close()
	}
	return nil
}

// Metadata set metadata to include in message
func (l *Logger) Metadata(metadata map[string]interface{}) {
	l.metadata = metadata
}

// SetLogLevel ..
func (l *Logger) SetLogLevel(level LogLevel) {
	l.Level = level
}

// SetConsole sets logger target to console
func (l *Logger) SetConsole() {
	if l.logfile != nil {
		err := l.logfile.Close()
		if err != nil {
			l.Warn("Failed to close old log file")
		}
		l.logfile = nil
	}

	l.write = func(message string) error {
		fmt.Print(message)
		return nil
	}
}

// SetFile sets logfile. If logger target was console, switch to file mode
func (l *Logger) SetFile(path string, permissions os.FileMode) error {
	newLogfile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, permissions)
	if err != nil {
		l.Warn("Couldn't open new log file, leaving the old one")
		return err
	}

	if l.logfile != nil {
		err = l.logfile.Close()
		if err != nil {
			l.Warn("Failed to close old log file")
		}
		l.logfile = newLogfile
		return nil
	}

	//target was console
	l.logfile = newLogfile
	l.write = func(message string) error {
		_, err := l.logfile.WriteString(message)
		return err
	}
	return nil
}

func (l *Logger) formatMetadata() (string, error) {
	//var build strings.Builder
	// Note: we need to support go-1.9.2 because of CentOS7
	var build bytes.Buffer
	if len(l.metadata) > 0 {
		joiner := ""
		for key, item := range l.metadata {
			_, err := fmt.Fprintf(&build, "%s%s: %v", joiner, key, item)
			if err != nil {
				return build.String(), err
			}
			if len(joiner) == 0 {
				joiner = ", "
			}
		}
	}
	// clear metadata for next use
	l.metadata = make(map[string]interface{})
	return build.String(), nil
}

func (l *Logger) writeRecord(level LogLevel, message string) error {
	metadata, err := l.formatMetadata()
	if err != nil {
		return err
	}

	//var build strings.Builder
	// Note: we need to support go-1.9.2 because of CentOS7
	var build bytes.Buffer
	if l.Timestamp {
		_, err = build.WriteString(time.Now().Format("2006-01-02 15:04:05 "))
	}

	_, err = build.WriteString(fmt.Sprintf("[%s] ", level))
	if err != nil {
		return nil
	}
	_, err = build.WriteString(message)
	if err != nil {
		return nil
	}
	if len(metadata) > 0 {
		_, err = build.WriteString(fmt.Sprintf(" [%s]", metadata))
		if err != nil {
			return nil
		}
	}
	_, err = build.WriteString("\n")
	if err != nil {
		return nil
	}
    l.metadata = Metadata{} //clear metadata
	err = l.write(build.String())
	return err
}

// Debug level debug
func (l *Logger) Debug(message string) error {
	if l.Level == DEBUG {
		return l.writeRecord(DEBUG, message)
	}
	return nil
}

// Info level info
func (l *Logger) Info(message string) error {
	if l.Level <= INFO {
		return l.writeRecord(INFO, message)
	}
	return nil
}

// Warn level warn
func (l *Logger) Warn(message string) error {
	if l.Level <= WARN {
		return l.writeRecord(WARN, message)
	}
	return nil
}

// Error level error
func (l *Logger) Error(message string) error {
	if l.Level <= ERROR {
		return l.writeRecord(ERROR, message)
	}
	return nil
}
