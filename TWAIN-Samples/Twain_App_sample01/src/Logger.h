#ifndef LOGGER_H
#define LOGGER_H

#include <string>
#include <cstdio>

class Logger {
public:
    static void Init(const char* logFileName = nullptr);
    static void Cleanup();
    static void Log(const char* format, ...);
    static void SetLogFileName(const char* logFileName);
    static void SetLogDirectory(const char* logDir);

private:
    static FILE* m_logFile;
    static std::string m_logFileName;
    static std::string m_logDirectory;
    static std::string GetCurrentTimeString();
    static const char* GetDefaultLogFileName();
    static std::string GetFullLogPath();
    static bool EnsureLogDirectory();
};

#endif // LOGGER_H