#include "http_server.h"
#include <afxwin.h>
#include <afxinet.h>
#include <afxsock.h>
#include <exception>
#include "../src/logger.h"

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

bool HttpServer::Start(int port) {
    if (m_isRunning) {
        TRACE(_T("HTTP server is already running\n"));
        return false;
    }

    try {
        // 如果WSA尚未初始化，则初始化
        if (!m_wsaInitialized) {
            WSADATA wsaData;
            int result = WSAStartup(MAKEWORD(2, 2), &wsaData);
            if (result != 0) {
                Logger::Log("ERROR: Failed to initialize WinSock. Error: %d", result);
                return false;
            }
            m_wsaInitialized = true;
            Logger::Log("WSA initialized successfully");
        }

        // 创建服务器套接字
        m_serverSocket = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
        if (m_serverSocket == INVALID_SOCKET) {
            TRACE(_T("Failed to create server socket. Error: %d\n"), WSAGetLastError());
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

        TRACE(_T("HTTP server started successfully on port %d\n"), port);
        return true;
    }
    catch (...) {
        TRACE(_T("Unknown exception while starting server\n"));
        if (m_serverSocket != INVALID_SOCKET) {
            closesocket(m_serverSocket);
            m_serverSocket = INVALID_SOCKET;
        }
        m_isRunning = false;
        return false;
    }
}

void HttpServer::Stop() {
    if (!m_isRunning) {
        return;
    }

    // 首先标记停止状态
    m_isRunning = false;
    
    Logger::Log("HTTP server stopping...");
    
    // 关闭套接字 - 这会导致accept调用返回错误，使线程能够退出循环
    if (m_serverSocket != INVALID_SOCKET) {
        Logger::Log("Closing server socket...");
        shutdown(m_serverSocket, SD_BOTH);
        closesocket(m_serverSocket);
        m_serverSocket = INVALID_SOCKET;
    }

    // 等待线程结束，设置合理的超时时间
    if (m_pThread) {
        Logger::Log("Waiting for server thread to terminate...");
        // 使用1秒超时，而不是无限等待，避免可能的死锁
        DWORD result = WaitForSingleObject(m_pThread->m_hThread, 1000);
        if (result == WAIT_TIMEOUT) {
            Logger::Log("Warning: Server thread did not terminate within timeout");
        }
        m_pThread = nullptr;
    }

    // 清理WSA，如果已初始化
    if (m_wsaInitialized) {
        Logger::Log("WSA cleanup in progress");
        WSACleanup();
        m_wsaInitialized = false;
        Logger::Log("WSA cleanup completed");
    }

    Logger::Log("HTTP server stopped");
}

UINT HttpServer::ServerThread(LPVOID pParam) {
    HttpServer* pServer = (HttpServer*)pParam;
    pServer->RunServer();
    return 0;
}

void HttpServer::RunServer() {
    // 使用成员函数获取主窗口句柄，避免直接使用成员变量
    HWND mainWnd = GetMainWindow();
    Logger::Log("RunServer: server thread started, main window handle: %p", mainWnd);
    
    while (m_isRunning) {
        SOCKET clientSocket = accept(m_serverSocket, nullptr, nullptr);
        if (clientSocket != INVALID_SOCKET) {
            // 每次都获取当前最新的主窗口句柄，而不是使用启动时的句柄
            HWND currentMainWnd = GetMainWindow();
            if (currentMainWnd && !::IsWindow(currentMainWnd)) {
                Logger::Log("warning: main window handle (%p) is invalid or NULL", currentMainWnd);
            }
            
            // 创建新线程处理客户端请求，传递主窗口句柄和socket
            auto* pData = new std::pair<HWND, SOCKET>(currentMainWnd, clientSocket);

            Logger::Log("RunServer: create client request thread, use main window handle: %p", currentMainWnd);
            AfxBeginThread(&HttpServer::ProcessRequest, (LPVOID)pData);
        }
        else {
            if (m_isRunning) {  // 只在服务器仍在运行时报告错误
                Logger::Log("Accept failed. Error: %d", WSAGetLastError());
            }
        }
        Sleep(100); // 避免CPU占用过高
    }
}

// 修改ProcessRequest为静态函数，以便作为线程函数使用
UINT HttpServer::ProcessRequest(LPVOID pParam) {
    // 添加异常处理，确保所有异常都被捕获
    try {
        // 检查参数是否有效
        if (!pParam) {
            Logger::Log("ProcessRequest: Invalid parameter (NULL)");
            return 1;
        }

        // 获取主窗口句柄和客户端socket
        auto* pPair = (std::pair<HWND, SOCKET>*)pParam;
        HWND hMainWnd = pPair->first;
        SOCKET clientSocket = pPair->second;
        
        Logger::Log("ProcessRequest: Received main window handle: %p", hMainWnd);
        
        delete pPair;  // 释放内存
        pPair = nullptr;  // 防止悬挂指针
        
        // 验证客户端socket
        if (clientSocket == INVALID_SOCKET) {
            Logger::Log("ProcessRequest: Invalid client socket");
            return 1;
        }
        
        char recvBuffer[1024];
        int recvLen = recv(clientSocket, recvBuffer, sizeof(recvBuffer) - 1, 0);
        if (recvLen > 0) {
            recvBuffer[recvLen] = '\0';
            std::string request(recvBuffer);
            
            Logger::Log("Received HTTP request: %s", request.c_str());
            
            bool messageSent = false;
            
            // 发送消息到主窗口 - 再次验证窗口有效性
            if (hMainWnd && ::IsWindow(hMainWnd)) {
                Logger::Log("Posting WM_HTTP_REQUEST message to main window (handle: %p)", hMainWnd);
                
                try {
                    CString* pRequest = new CString(request.c_str());
                    if (!pRequest) {
                        Logger::Log("Failed to allocate memory for request");
                    } else {
                        BOOL result = ::PostMessage(hMainWnd, WM_HTTP_REQUEST, (WPARAM)pRequest, 0);
                        if (!result) {
                            Logger::Log("PostMessage failed. Error: %d", GetLastError());
                            delete pRequest;  // 确保内存被释放
                            pRequest = nullptr;
                        } else {
                            Logger::Log("PostMessage succeeded");
                            messageSent = true;
                            
                            // 等待一小段时间，让主窗口有机会处理消息
                            Sleep(100);
                        }
                    }
                } catch (std::exception& e) {
                    Logger::Log("Exception while posting message: %s", e.what());
                } catch (...) {
                    Logger::Log("Unknown exception while posting message");
                }
            } else {
                Logger::Log("No valid main window handle available (handle: %p, IsWindow: %d)", 
                        hMainWnd, hMainWnd ? ::IsWindow(hMainWnd) : 0);
            }
            
            // 无论主窗口是否处理了消息，都发送HTTP响应
            std::string response;
            
            if (messageSent) {
                response = "HTTP/1.1 200 OK\r\n"
                        "Content-Type: text/html\r\n"
                        "Connection: close\r\n"
                        "\r\n"
                        "<html><body>"
                        "<h1>TWAIN HTTP Server</h1>"
                        "<p>Request received and processed by main window.</p>"
                        "</body></html>";
            } else {
                response = "HTTP/1.1 200 OK\r\n"
                        "Content-Type: text/html\r\n"
                        "Connection: close\r\n"
                        "\r\n"
                        "<html><body>"
                        "<h1>TWAIN HTTP Server</h1>"
                        "<p>Request received but main window unavailable.</p>"
                        "</body></html>";
            }
            
            send(clientSocket, response.c_str(), response.length(), 0);
            Logger::Log("Response sent to client");
        } else if (recvLen == 0) {
            Logger::Log("Client closed connection before sending data");
        } else {
            Logger::Log("Error receiving data: %d", WSAGetLastError());
        }
        
        // 关闭客户端socket
        if (clientSocket != INVALID_SOCKET) {
            shutdown(clientSocket, SD_BOTH);  // 确保完全关闭
            closesocket(clientSocket);
            clientSocket = INVALID_SOCKET;
        }
        
        return 0;
    } catch (std::exception& e) {
        Logger::Log("Exception in ProcessRequest: %s", e.what());
    } catch (...) {
        Logger::Log("Unknown exception in ProcessRequest");
    }
    
    return 1;  // 发生错误时返回非零值
}

void HttpServer::SendResponse(SOCKET clientSocket, const std::string& response)
{
    send(clientSocket, response.c_str(), response.length(), 0);
}

void HttpServer::PostMessageToMain(UINT message, WPARAM wParam, LPARAM lParam)
{
    HWND hMainWnd = GetMainWindow();
    if (hMainWnd && ::IsWindow(hMainWnd))
    {
        ::PostMessage(hMainWnd, message, wParam, lParam);
    }
}
