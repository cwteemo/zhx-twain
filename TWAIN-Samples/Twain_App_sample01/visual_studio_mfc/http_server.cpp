#include "stdafx.h"
#include "http_server.h"
#include "..\src\logger.h"
#include <afxwin.h>
#include <afxinet.h>
#include <afxsock.h>
#include <exception>
#include <fstream>
#include <ctime>
#include <mutex>

// 前向声明
class CInternetServerContext;

// 自定义HTTP服务器类
class CInternetServer : public CObject {
public:
    CInternetServer(CInternetSession* pSession) : m_pSession(pSession) {}
    virtual ~CInternetServer() {}

    bool Create(int port) {
        // 创建服务器套接字
        return m_socket.Create(port) && m_socket.Listen(5);
    }

    void Close() {
        m_socket.Close();
    }

    CInternetServerContext* GetContext();

protected:
    CInternetSession* m_pSession;
    CSocket m_socket;
};

// 自定义HTTP服务器上下文类
class CInternetServerContext : public CObject {
public:
    CInternetServerContext(CInternetServer* pServer, CSocket& socket) 
        : m_pServer(pServer), m_socket(socket) {}
    virtual ~CInternetServerContext() {
        m_socket.Close();
    }

    CString GetRequest() {
        char buffer[1024];
        int bytesRead = m_socket.Receive(buffer, sizeof(buffer) - 1);
        if (bytesRead > 0) {
            buffer[bytesRead] = '\0';
            return CString(buffer);
        }
        return _T("");
    }

    void WriteString(const CString& str) {
        m_socket.Send((LPCTSTR)str, str.GetLength() * sizeof(TCHAR));
    }

    void Close() {
        m_socket.Close();
    }

protected:
    CInternetServer* m_pServer;
    CSocket& m_socket;
};

// 实现 GetContext 方法
CInternetServerContext* CInternetServer::GetContext() {
    CSocket clientSocket;
    if (m_socket.Accept(clientSocket)) {
        return new CInternetServerContext(this, clientSocket);
    }
    return nullptr;
}

HttpServer::HttpServer() 
    : m_serverSocket(INVALID_SOCKET)
    , m_isRunning(false)
    , m_port(8080)
    , m_pThread(nullptr)
    , m_twainReadyEvent(CreateEvent(NULL, TRUE, FALSE, NULL))
{
    // 初始化 WinSock
    WSADATA wsaData;
    if (WSAStartup(MAKEWORD(2, 2), &wsaData) != 0) {
        Logger::Log("Failed to initialize WinSock in constructor");
    }
}

HttpServer::~HttpServer() {
    Stop();
    if (m_twainReadyEvent) {
        CloseHandle(m_twainReadyEvent);
    }
    WSACleanup();
}

bool HttpServer::Start(int port) {
    if (m_isRunning) {
        Logger::Log("HTTP server is already running");
        return false;
    }

    try {
        // 创建服务器套接字
        m_serverSocket = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
        if (m_serverSocket == INVALID_SOCKET) {
            Logger::Log("Failed to create server socket. Error: %d", WSAGetLastError());
            return false;
        }

        // 设置套接字选项
        int opt = 1;
        if (setsockopt(m_serverSocket, SOL_SOCKET, SO_REUSEADDR, (char*)&opt, sizeof(opt)) == SOCKET_ERROR) {
            TRACE(_T("Failed to set socket options. Error: %d\n"), WSAGetLastError());
            closesocket(m_serverSocket);
            m_serverSocket = INVALID_SOCKET;
            return false;
        }

        // 绑定地址和端口
        sockaddr_in serverAddr;
        serverAddr.sin_family = AF_INET;
        serverAddr.sin_addr.s_addr = INADDR_ANY;
        serverAddr.sin_port = htons(port);

        if (bind(m_serverSocket, (sockaddr*)&serverAddr, sizeof(serverAddr)) == SOCKET_ERROR) {
            TRACE(_T("Failed to bind socket. Error: %d\n"), WSAGetLastError());
            closesocket(m_serverSocket);
            m_serverSocket = INVALID_SOCKET;
            return false;
        }

        // 开始监听
        if (listen(m_serverSocket, 5) == SOCKET_ERROR) {
            TRACE(_T("Failed to start listening. Error: %d\n"), WSAGetLastError());
            closesocket(m_serverSocket);
            m_serverSocket = INVALID_SOCKET;
            return false;
        }

        m_port = port;
        m_isRunning = true;

        // 创建服务器线程
        m_pThread = AfxBeginThread(ServerThread, this);
        if (!m_pThread) {
            TRACE(_T("Failed to create server thread. Error: %d\n"), GetLastError());
            closesocket(m_serverSocket);
            m_serverSocket = INVALID_SOCKET;
            m_isRunning = false;
            return false;
        }

        Logger::Log("HTTP server started successfully on port %d", port);
        return true;
    }
    catch (...) {
        Logger::Log("Unknown exception while starting server");
        if (m_serverSocket != INVALID_SOCKET) {
            closesocket(m_serverSocket);
            m_serverSocket = INVALID_SOCKET;
        }
        m_isRunning = false;
        return false;
    }
}

void HttpServer::SetTwainApp(TwainApp* pTwainApp) {
    std::lock_guard<std::mutex> lock(m_mutex);
    try {
        // Always reset the event first
        ResetEvent(m_twainReadyEvent);
        Logger::Log("SetTwainApp: Event reset");

        if (!pTwainApp) {
            Logger::Log("SetTwainApp: Received null pointer");
            m_pTwainApp.reset();
            return;
        }

        // Store current DSM state for logging
        int oldState = m_pTwainApp ? m_pTwainApp->m_DSMState : 0;
        
        // Create a new shared pointer with custom deleter
        m_pTwainApp = std::shared_ptr<TwainApp>(pTwainApp, [](TwainApp* p) {
            Logger::Log("TWAIN application shared_ptr custom deleter called");
        });
        
        // Verify the new state
        if (m_pTwainApp && m_pTwainApp.get() == pTwainApp) {
            Logger::Log("SetTwainApp: TWAIN pointer set successfully (old state: %d, new state: %d)", 
                       oldState, pTwainApp->m_DSMState);
            
            if (pTwainApp->m_DSMState >= 3) {
                Logger::Log("SetTwainApp: Setting ready event (valid DSM state: %d)", pTwainApp->m_DSMState);
                SetEvent(m_twainReadyEvent);
            } else {
                Logger::Log("SetTwainApp: Not setting ready event (invalid DSM state: %d)", pTwainApp->m_DSMState);
            }
        } else {
            Logger::Log("SetTwainApp: Failed to set TWAIN pointer");
            m_pTwainApp.reset();
        }
    }
    catch (const std::exception& e) {
        Logger::Log("Exception in SetTwainApp: %s", e.what());
        m_pTwainApp.reset();
        ResetEvent(m_twainReadyEvent);
    }
    catch (...) {
        Logger::Log("Unknown exception in SetTwainApp");
        m_pTwainApp.reset();
        ResetEvent(m_twainReadyEvent);
    }
}

bool HttpServer::WaitForTwainApp(int timeoutMs) {
    Logger::Log("Waiting for TWAIN application to be ready...");
    
    // First check if we already have a valid TWAIN app
    {
        std::lock_guard<std::mutex> lock(m_mutex);
        if (m_pTwainApp && m_pTwainApp->m_DSMState >= 3) {
            Logger::Log("TWAIN application is already ready (DSM state: %d)", m_pTwainApp->m_DSMState);
            return true;
        }
    }
    
    // If not, wait for the event
    DWORD result = WaitForSingleObject(m_twainReadyEvent, timeoutMs);
    if (result == WAIT_OBJECT_0) {
        std::lock_guard<std::mutex> lock(m_mutex);
        if (m_pTwainApp && m_pTwainApp->m_DSMState >= 3) {
            Logger::Log("TWAIN application is ready after wait");
            return true;
        }
        Logger::Log("Event was set but TWAIN app is not valid");
    }
    
    Logger::Log("Timeout waiting for TWAIN application");
    return false;
}

std::string HttpServer::GetScannerList() {
    Logger::Log("GetScannerList: 开始获取扫描仪列表请求");
    
    if (!WaitForTwainApp()) {
        Logger::Log("GetScannerList: TWAIN 应用程序未就绪");
        return "{\"error\": \"TWAIN application not ready\"}";
    }

    std::lock_guard<std::mutex> lock(m_mutex);
    
    if (!m_pTwainApp) {
        Logger::Log("GetScannerList: TWAIN 应用程序指针为空");
        return "{\"error\": \"TWAIN application unavailable\"}";
    }

    try {
        Logger::Log("GetScannerList: 当前 DSM 状态: %d", m_pTwainApp->m_DSMState);
        
        if (m_pTwainApp->m_DSMState < 3) {
            Logger::Log("GetScannerList: DSM 状态无效，无法获取扫描仪列表");
            return "{\"error\": \"TWAIN DSM not in correct state\"}";
        }

        std::vector<std::string> deviceList;
        pTW_IDENTITY pID = nullptr;
        int i = 0;

        Logger::Log("GetScannerList: 开始枚举扫描仪设备...");
        while ((pID = m_pTwainApp->getDataSource((TW_INT16)i)) != nullptr) {
            std::string scannerName = pID->ProductName;
            deviceList.push_back(scannerName);
            Logger::Log("GetScannerList: 找到扫描仪 %d: %s", i, scannerName.c_str());
            i++;
        }

        // 构建 JSON 响应
        std::string response = "{\"scanners\":[";
        for (size_t i = 0; i < deviceList.size(); ++i) {
            if (i > 0) response += ",";
            response += "\"" + deviceList[i] + "\"";
        }
        response += "]}";

        Logger::Log("GetScannerList: 成功获取到 %zu 个扫描仪", deviceList.size());
        return response;
    }
    catch (const std::exception& e) {
        Logger::Log("GetScannerList: 发生异常: %s", e.what());
        return "{\"error\": \"Internal error while retrieving scanner list\"}";
    }
    catch (...) {
        Logger::Log("GetScannerList: 发生未知异常");
        return "{\"error\": \"Unknown error while retrieving scanner list\"}";
    }
}

void HttpServer::Stop() {
    Logger::Log("Stopping HTTP server...");
    {
        std::lock_guard<std::mutex> lock(m_mutex);
        if (m_pTwainApp && m_pTwainApp->m_DSMState >= 3) {
            Logger::Log("Disconnecting from DSM...");
            m_pTwainApp->disconnectDSM();
        }
        m_pTwainApp.reset();  // 释放共享指针
        ResetEvent(m_twainReadyEvent);
    }

    m_isRunning = false;
    
    // 关闭套接字
    if (m_serverSocket != INVALID_SOCKET) {
        closesocket(m_serverSocket);
        m_serverSocket = INVALID_SOCKET;
    }

    // 等待线程结束
    if (m_pThread) {
        WaitForSingleObject(m_pThread->m_hThread, INFINITE);
        m_pThread = nullptr;
    }

    Logger::Log("HTTP server stopped");
}

UINT HttpServer::ServerThread(LPVOID pParam) {
    HttpServer* pServer = (HttpServer*)pParam;
    pServer->RunServer();
    return 0;
}

void HttpServer::RunServer() {
    while (m_isRunning) {
        SOCKET clientSocket = accept(m_serverSocket, nullptr, nullptr);
        if (clientSocket != INVALID_SOCKET) {
            ProcessRequest(clientSocket);
        }
        else {
            if (m_isRunning) {  // 只在服务器仍在运行时报告错误
                TRACE(_T("Accept failed. Error: %d\n"), WSAGetLastError());
            }
        }
        Sleep(100); // 避免CPU占用过高
    }
}

void HttpServer::ProcessRequest(SOCKET clientSocket) {
    try {
        // 设置接收超时
        DWORD timeout = 5000; // 5 seconds
        setsockopt(clientSocket, SOL_SOCKET, SO_RCVTIMEO, (char*)&timeout, sizeof(timeout));

        // 读取请求
        char buffer[1024];
        int bytesRead = recv(clientSocket, buffer, sizeof(buffer) - 1, 0);
        if (bytesRead > 0) {
            buffer[bytesRead] = '\0';
            Logger::Log("Received request: %s", buffer);

            // 解析请求路径
            CString request(buffer);
            std::string response;
            
            if (request.Find(_T("GET /scanners")) != -1) {
                Logger::Log("Processing /scanners request");
                response = GetScannerList();
                Logger::Log("Scanner list response prepared: %s", response.c_str());
            }
            else {
                response = "123";
            }

            // 构建 HTTP 响应头
            std::string httpResponse = 
                "HTTP/1.1 200 OK\r\n"
                "Content-Type: application/json\r\n"
                "Access-Control-Allow-Origin: *\r\n"
                "Connection: close\r\n"
                "\r\n";
            
            // 添加响应体
            httpResponse += response;

            // 发送响应
            int bytesSent = send(clientSocket, httpResponse.c_str(), static_cast<int>(httpResponse.length()), 0);
            Logger::Log("Response sent: %d bytes", bytesSent);
        }
        else if (bytesRead == 0) {
            Logger::Log("Client closed connection");
        }
        else {
            Logger::Log("Receive failed. Error: %d", WSAGetLastError());
        }
    }
    catch (const std::exception& e) {
        Logger::Log("Exception in ProcessRequest: %s", e.what());
    }
    catch (...) {
        Logger::Log("Unknown exception in ProcessRequest");
    }

    closesocket(clientSocket);
} 