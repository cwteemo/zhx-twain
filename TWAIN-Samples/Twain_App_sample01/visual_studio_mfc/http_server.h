#pragma once

#include <afxwin.h>
#include <afxinet.h>
#include <afxsock.h>
#include "..\src\TwainApp.h"
#include <string>
#include <mutex>
#include <memory>
#include <atomic>

class HttpServer {
public:
    HttpServer();
    virtual ~HttpServer();

    bool Start(int port = 8080);
    void Stop();
    void SetTwainApp(TwainApp* pTwainApp);
    std::string GetScannerList();

private:
    static UINT ServerThread(LPVOID pParam);
    void RunServer();
    void ProcessRequest(SOCKET clientSocket);
    bool WaitForTwainApp(int timeoutMs = 5000);

    SOCKET m_serverSocket;
    std::atomic<bool> m_isRunning;
    int m_port;
    CWinThread* m_pThread;
    std::shared_ptr<TwainApp> m_pTwainApp;
    std::mutex m_mutex;
    HANDLE m_twainReadyEvent;
}; 