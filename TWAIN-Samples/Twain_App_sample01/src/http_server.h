/***************************************************************************
 * HTTP服务器接口（纯Win32实现，不依赖MFC）
 * 用于在DLL中提供HTTP服务，使任何语言都可以通过HTTP调用TWAIN接口
 * 
 * 参考：https://github.com/twain/twain-samples
 ***************************************************************************/

#pragma once

#ifdef TWH_CMP_MSC
#include <windows.h>
#include <winsock2.h>
#include <ws2tcpip.h>
#include <string>
#include <vector>
#include <map>
#include <thread>
#include <atomic>
#include <mutex>

#pragma comment(lib, "ws2_32.lib")

// HTTP请求结构
struct HttpRequest {
    std::string method;      // GET, POST等
    std::string path;        // /api/devices等
    std::string query;       // 查询参数
    std::string body;        // POST请求体
    std::map<std::string, std::string> headers;
    std::map<std::string, std::string> params;  // 解析后的参数
};

// HTTP服务器类（纯Win32实现）
class HttpServer {
public:
    HttpServer();
    ~HttpServer();

    // 启动HTTP服务器
    // @param port 端口号（默认8080）
    // @return true表示成功，false表示失败
    bool Start(int port = 8080);

    // 停止HTTP服务器
    void Stop();

    // 检查服务器是否运行中
    bool IsRunning() const { return m_isRunning.load(); }

    // 获取端口号
    int GetPort() const { return m_port; }

private:
    // 服务器线程函数
    static DWORD WINAPI ServerThreadProc(LPVOID lpParam);
    void ServerThread();

    // 处理客户端连接
    void HandleClient(SOCKET clientSocket);

    // 解析HTTP请求
    bool ParseRequest(const std::string& request, HttpRequest& req);

    // 构建HTTP响应
    std::string BuildResponse(int statusCode, const std::string& contentType, const std::string& body);
    std::string BuildJSONResponse(const std::string& json);
    std::string BuildErrorResponse(int statusCode, const std::string& message);

    // 处理API请求
    std::string HandleAPI(const HttpRequest& req);

    // API处理函数
    std::string HandleGetDevices();                    // GET /api/devices
    std::string HandleScan(const HttpRequest& req);    // POST /api/scan
    std::string HandleScanBatch(const HttpRequest& req); // POST /api/scan/batch (连续扫描)
    std::string HandleGetStatus();                    // GET /api/status
    std::string HandleGetScanResult(const HttpRequest& req); // GET /api/scan/result/:id

    // 辅助函数
    std::string URLDecode(const std::string& str);
    std::string GetJSONValue(const std::string& json, const std::string& key);
    std::string EscapeJSON(const std::string& str);
    std::vector<std::string> SplitString(const std::string& str, char delimiter);

private:
    SOCKET m_serverSocket;
    std::atomic<bool> m_isRunning;
    int m_port;
    HANDLE m_threadHandle;
    bool m_wsaInitialized;
    std::mutex m_mutex;
};

// 全局HTTP服务器实例
extern HttpServer* g_pHttpServer;

// C接口导出（供外部调用）
extern "C" {
    // 启动HTTP服务器
    __declspec(dllexport) int zhx_HttpServer_Start(int port);
    
    // 停止HTTP服务器
    __declspec(dllexport) void zhx_HttpServer_Stop();
    
    // 检查服务器状态
    __declspec(dllexport) int zhx_HttpServer_IsRunning();
    
    // 获取服务器端口
    __declspec(dllexport) int zhx_HttpServer_GetPort();
}

#endif // TWH_CMP_MSC
