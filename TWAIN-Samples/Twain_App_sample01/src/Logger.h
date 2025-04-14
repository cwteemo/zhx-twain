#ifndef LOGGER_H
#define LOGGER_H

#include <string>
#include <cstdio>

class Logger {
public:
    static void Init();
    static void Cleanup();
    static void Log(const char* format, ...);

private:
    static FILE* m_logFile;
    static std::string GetCurrentTimeString();
};

#endif // LOGGER_H