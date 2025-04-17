#pragma once

#include <afxwin.h>
#include <afxinet.h>
#include <afxsock.h>
#include <string>
#include <vector>
#include <mutex>
#include <condition_variable>
#include <atomic>
#include "../src/logger.h"
#include <windows.h>
#include <process.h>
#include <afx.h>
#include <map>

// 在CYourMainDlg类的头文件中，定义一个消息宏
#define WM_RECEIVE_DATA (WM_USER + 100)

// 定义消息ID - 确保不与WM_RECEIVE_DATA冲突
#define WM_HTTP_REQUEST (WM_USER + 101)
#define WM_TCP_DATA_RECEIVED (WM_USER + 103)
#define WM_CONNECT_SCANNER (WM_USER + 102)
#define WM_START_SCAN (WM_USER + 104)

// 定义扫描参数结构体
struct ScanParams
{
    char scannerName[256];
    char extension[32];
    bool showSettings;
    char savePath[MAX_PATH]; // 添加保存路径字段
};

class HttpServer {
public:
    HttpServer() : m_hMainWnd(NULL), m_serverSocket(INVALID_SOCKET), m_isRunning(false), m_port(8080), m_pThread(nullptr), m_running(false), m_wsaInitialized(false) {
        // WSA初始化移至Start方法
    }
    
    ~HttpServer() {
        // 确保服务器停止
        try {
            Logger::Log("HttpServer destructor called");
            Stop();
            // WSA清理移至Stop方法
            Logger::Log("HttpServer cleanup completed");
        } catch (const std::exception& e) {
            Logger::Log("Exception during HttpServer destruction: %s", e.what());
        } catch (...) {
            Logger::Log("Unknown exception during HttpServer destruction");
        }
    }

    bool Start(int port);
    void Stop();
    void SetMainWindow(HWND hWnd) { 
        // 手动加锁而不是使用lock_guard
        m_mutex.lock();
        
        m_hMainWnd = hWnd; 
        Logger::Log("HTTP server main window handle set to: %p", hWnd);
        // 验证窗口句柄是否有效
        if (hWnd && ::IsWindow(hWnd)) {
            Logger::Log("Main window handle is valid");
        } else {
            Logger::Log("WARNING: Main window handle is invalid or NULL");
        }
        
        // 手动解锁
        m_mutex.unlock();
    }
    
    // 设置最后的HTTP响应内容
    void SetLastResponse(const CString& response) {
        m_mutex.lock();
        m_lastResponse = response;
        m_mutex.unlock();
        Logger::Log("Last HTTP response set: %s", (LPCTSTR)response);
    }
    
    // 获取最后设置的响应内容
    CString GetLastResponse() {
        m_mutex.lock();
        CString response = m_lastResponse;
        m_mutex.unlock();
        return response;
    }
    
    // 添加获取主窗口句柄的方法
    HWND GetMainWindow() {
        // 在非const方法中使用锁，避免任何潜在问题
        m_mutex.lock();
        HWND hwnd = m_hMainWnd;
        m_mutex.unlock();
        return hwnd; 
    }

private:
    static UINT ServerThread(LPVOID pParam);
    static UINT ProcessRequest(LPVOID pParam);
    void RunServer();
    void SendResponse(SOCKET clientSocket, const std::string& response);
    void PostMessageToMain(UINT message, WPARAM wParam, LPARAM lParam);

    HWND m_hMainWnd;
    SOCKET m_serverSocket;
    bool m_isRunning;
    int m_port;
    CWinThread* m_pThread;
    std::atomic<bool> m_running;
    std::mutex m_mutex;  // 移除mutable修饰符，使用非const方法
    std::condition_variable m_cv;
    bool m_wsaInitialized;  // 标记WSA是否已初始化
    CString m_lastResponse;  // 保存最后的HTTP响应内容
};

// 函数声明
void ProcessRequest(HttpServer* pHttpServer, SOCKET clientSocket, const std::string& request);
void ParseHttpRequest(const std::string& request, std::string& url, std::string& params, 
                      std::map<std::string, std::string>& paramsMap, std::string& requestType);
std::string BuildHttpResponse(HttpServer* pHttpServer, HWND hMainWnd, bool scannerListRequested, 
                             bool containsScannersKeyword, const std::string& params, const std::string& request);
std::string GetScannerListResponse(HttpServer* pHttpServer, HWND hMainWnd);
std::string BuildScannerOptionsResponse(HttpServer* pHttpServer, HWND hMainWnd, const std::map<std::string, std::string>& paramsMap);
std::string BuildScanResponse(HttpServer* pHttpServer, HWND hMainWnd, const std::string& scannerName, 
                             const std::string& extension, const std::string& showSetting,
                             const std::map<std::string, std::string>& paramsMap); 