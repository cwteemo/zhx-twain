/***************************************************************************
* Copyright 2007 TWAIN Working Group:  
*   Adobe Systems Incorporated, AnyDoc Software Inc., Eastman Kodak Company, 
*   Fujitsu Computer Products of America, JFL Peripheral Solutions Inc., 
*   Ricoh Corporation, and Xerox Corporation.
* All rights reserved.
*
* Redistribution and use in source and binary forms, with or without
* modification, are permitted provided that the following conditions are met:
*     * Redistributions of source code must retain the above copyright
*       notice, this list of conditions and the following disclaimer.
*     * Redistributions in binary form must reproduce the above copyright
*       notice, this list of conditions and the following disclaimer in the
*       documentation and/or other materials provided with the distribution.
*     * Neither the name of the TWAIN Working Group nor the
*       names of its contributors may be used to endorse or promote products
*       derived from this software without specific prior written permission.
*
* THIS SOFTWARE IS PROVIDED BY TWAIN Working Group ``AS IS'' AND ANY
* EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
* WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
* DISCLAIMED. IN NO EVENT SHALL TWAIN Working Group BE LIABLE FOR ANY
* DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
* (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
* LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
* ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
* (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
* SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*
***************************************************************************/

// Windows Socket headers
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")

/**
* @file mfc32DlgMain.cpp
* Implementation file for dialog for the MFC TWAIN App Sample application
* @author JFL Peripheral Solutions Inc.
* @date October 2007
*/

#include "stdafx.h"
#include "mfc.h"
#include "mfcDlgMain.h"
#include "mfcDlgConfigure.h"
#include "..\src\logger.h" 
#include "http_server.h"  // 添加 HTTP 服务器头文件

#include "twain.h"
#include "..\src\twainapp.h"
#include "..\src\dsminterface.h"
#include "TwainString.h"
#include ".\TW_Enum_Dlg.h"

#ifdef _DEBUG
#define new DEBUG_NEW
#endif

// 声明辅助函数
const char* GetWSAErrorString(int error);

// CAboutDlg dialog used for App About

class CAboutDlg : public CDialog
{
public:
  CAboutDlg();

// Dialog Data
  enum { IDD = IDD_ABOUTBOX };

  protected:
  virtual void DoDataExchange(CDataExchange* pDX);    // DDX/DDV support

// Implementation
protected:
  DECLARE_MESSAGE_MAP()
};

CAboutDlg::CAboutDlg() : CDialog(CAboutDlg::IDD)
{
}

void CAboutDlg::DoDataExchange(CDataExchange* pDX)
{
  CDialog::DoDataExchange(pDX);
}

BEGIN_MESSAGE_MAP(CAboutDlg, CDialog)
END_MESSAGE_MAP()


// CmfcDlgMain dialog



CmfcDlgMain::CmfcDlgMain(CWnd* pParent /*=NULL*/)
  : CDialog(CmfcDlgMain::IDD, pParent)
  , _pTWAINApp(NULL)
  , m_sStc_DS(_T(""))
  , m_httpServer(nullptr)  // 不再创建新的HTTP服务器实例
{
  m_hIcon = AfxGetApp()->LoadIcon(IDR_MAINFRAME);
}

void CmfcDlgMain::DoDataExchange(CDataExchange* pDX)
{
  CDialog::DoDataExchange(pDX);
  DDX_Text(pDX, IDC_STC_DS, m_sStc_DS);
  DDX_Control(pDX, IDL_DS, m_lst_DS);
  DDX_Control(pDX, IDB_CONNECT_DS, m_btn_Connect_DS);
  DDX_Control(pDX, IDB_DEFAULT_DS, m_btn_Default_DS);
}

BEGIN_MESSAGE_MAP(CmfcDlgMain, CDialog)
  ON_WM_SYSCOMMAND()
  ON_WM_PAINT()
  ON_WM_QUERYDRAGICON()
  ON_WM_DESTROY()
  ON_LBN_SELCHANGE(IDL_DS, OnLbnSelchangeDS)
  ON_LBN_DBLCLK(IDL_DS, OnLbnDblclkDs)
  ON_BN_CLICKED(IDB_CONNECT_DS, OnBnClickedConnectDs)
  ON_BN_CLICKED(IDB_DEFAULT_DS, OnBnClickedDefaultDs)
  ON_MESSAGE(WM_HTTP_REQUEST, OnHttpRequest)
  ON_MESSAGE(WM_TCP_DATA_RECEIVED, OnTcpData)
  ON_MESSAGE(WM_RECEIVE_DATA, OnReceiveData)
  ON_MESSAGE(WM_CONNECT_SCANNER, OnConnectScanner)
END_MESSAGE_MAP()


// CmfcDlgMain message handlers

BOOL CmfcDlgMain::OnInitDialog()
{
    // 添加日志记录，但不再初始化（由应用类负责）
    Logger::Log("=== Dialog Initialization Started ===");

    CDialog::OnInitDialog();

    // Add "About..." menu item to system menu.
    ASSERT((IDM_ABOUTBOX & 0xFFF0) == IDM_ABOUTBOX);
    ASSERT(IDM_ABOUTBOX < 0xF000);

    CMenu* pSysMenu = GetSystemMenu(FALSE);
    if (pSysMenu != NULL)
    {
        CString strAboutMenu;
        strAboutMenu.LoadString(IDS_ABOUTBOX);
        if (!strAboutMenu.IsEmpty())
        {
            pSysMenu->AppendMenu(MF_SEPARATOR);
            pSysMenu->AppendMenu(MF_STRING, IDM_ABOUTBOX, strAboutMenu);
        }
    }

    // Set the icon for this dialog.
    SetIcon(m_hIcon, TRUE);     // Set big icon
    SetIcon(m_hIcon, FALSE);    // Set small icon

    try {
        // 初始化HTTP服务器
        m_httpServer = new HttpServer();
        if (m_httpServer) {
            Logger::Log("HTTP server instance created");
            m_httpServer->SetMainWindow(GetSafeHwnd());
            
            // 保存HttpServer实例到窗口用户数据
            ::SetWindowLongPtr(GetSafeHwnd(), GWLP_USERDATA, (LONG_PTR)m_httpServer);
            Logger::Log("HTTP server instance stored in window user data");
            
            if (m_httpServer->Start(8080)) {
                Logger::Log("HTTP server started on port 8080");
            } else {
                Logger::Log("Failed to start HTTP server");
            }
        } else {
            Logger::Log("Failed to create HTTP server instance");
        }

        // 修改现有的 TWAIN 初始化代码
        _pTWAINApp = new TwainApp(m_hWnd);
        if (!_pTWAINApp) {
            Logger::Log("Failed to create TwainApp instance");
            return FALSE;
        }
        Logger::Log("TwainApp instance created successfully");

        // Set up our Unique Application Identity
        TW_IDENTITY *pAppID = _pTWAINApp->getAppIdentity();

        pAppID->Version.MajorNum = 2;
        pAppID->Version.MinorNum = 1;
        pAppID->Version.Language = TWLG_ENGLISH_CANADIAN;
        pAppID->Version.Country = TWCY_CANADA;
        SSTRCPY(pAppID->Version.Info, sizeof(pAppID->Version.Info), "2.1.1");
        pAppID->ProtocolMajor = TWON_PROTOCOLMAJOR;
        pAppID->ProtocolMinor = TWON_PROTOCOLMINOR;
        pAppID->SupportedGroups = DF_APP2 | DG_IMAGE | DG_CONTROL;
        SSTRCPY(pAppID->Manufacturer, sizeof(pAppID->Manufacturer), "TWAIN Working Group");
        SSTRCPY(pAppID->ProductFamily, sizeof(pAppID->ProductFamily), "Sample");
        SSTRCPY(pAppID->ProductName, sizeof(pAppID->ProductName), "MFC Supported Caps");

        Logger::Log("Application Identity configured");

        CEdit *pWnd = NULL;
        pWnd = (CEdit*)GetDlgItem(IDC_STC_DS);
        if(pWnd)
        {
            pWnd->SetTabStops(60);
        }

        //Connect to the DSM just to update list
        Logger::Log("Attempting to connect to DSM...");
        _pTWAINApp->connectDSM();
        if(_pTWAINApp->m_DSMState >= 3)
        {
            Logger::Log("DSM connected successfully");
            PopulateDSList();

            // 检查HTTP服务器状态而不是重新启动它
            if (m_httpServer) {
                Logger::Log("Using HTTP server from application instance (port 8080)");
            } else {
                Logger::Log("No HTTP server available");
            }
        }
        else
        {
            Logger::Log("Failed to connect to DSM");
        }

        Logger::Log("OnInitDialog completed successfully");
        return TRUE;
    }
    catch (const std::exception& e) {
        Logger::Log("Exception in OnInitDialog: %s", e.what());
        return FALSE;
    }
    catch (...) {
        Logger::Log("Unknown exception in OnInitDialog");
        return FALSE;
    }
}

void CmfcDlgMain::OnSysCommand(UINT nID, LPARAM lParam)
{
  if ((nID & 0xFFF0) == IDM_ABOUTBOX)
  {
    CAboutDlg dlgAbout;
    dlgAbout.DoModal();
  }
  else
  {
    CDialog::OnSysCommand(nID, lParam);
  }
}

// If you add a minimize button to your dialog, you will need the code below
//  to draw the icon.  For MFC applications using the document/view model,
//  this is automatically done for you by the framework.

void CmfcDlgMain::OnPaint() 
{
  if (IsIconic())
  {
    CPaintDC dc(this); // device context for painting

    SendMessage(WM_ICONERASEBKGND, reinterpret_cast<WPARAM>(dc.GetSafeHdc()), 0);

    // Center icon in client rectangle
    int cxIcon = GetSystemMetrics(SM_CXICON);
    int cyIcon = GetSystemMetrics(SM_CYICON);
    CRect rect;
    GetClientRect(&rect);
    int x = (rect.Width() - cxIcon + 1) / 2;
    int y = (rect.Height() - cyIcon + 1) / 2;

    // Draw the icon
    dc.DrawIcon(x, y, m_hIcon);
  }
  else
  {
    CDialog::OnPaint();
  }
}

// The system calls this function to obtain the cursor to display while the user drags
//  the minimized window.
HCURSOR CmfcDlgMain::OnQueryDragIcon()
{
  return static_cast<HCURSOR>(m_hIcon);
}

void CmfcDlgMain::OnDestroy()
{
  Logger::Log("=== Application Shutting Down ===");
  
  // 只设置指针为null，不要尝试停止或删除HTTP服务器
  // 因为它由应用程序类管理
  m_httpServer = nullptr;
  TRACE(_T("HTTP server pointer cleared\n"));

  CDialog::OnDestroy();
  if(_pTWAINApp)
  {
    Logger::Log("Cleaning up TWAIN application");
    _pTWAINApp->exit();
    delete _pTWAINApp;
    _pTWAINApp = NULL;
  }

  Logger::Log("=== Application Ended ===");
  Logger::Cleanup();
  return;
}

void CmfcDlgMain::PopulateDSList()
{
  pTW_IDENTITY pID = NULL;
  int   i = 0;
  int   index = 0;
  int   nDefault = -1;

  // Emply the list the refill
  m_lst_DS.ResetContent();

  if( NULL != (pID = _pTWAINApp->getDefaultDataSource()) ) // Get Default
  {
    nDefault = pID->Id;
  }

  while( NULL != (pID = _pTWAINApp->getDataSource((TW_INT16)i)) )
  {
    index = m_lst_DS.AddString( pID->ProductName );
    if(LB_ERR == index)
    {
      break;
    }

    m_lst_DS.SetItemData( index, i );

    if(nDefault == (int)pID->Id)
    {
      m_lst_DS.SetCurSel(index);
    }

    i++;
  }

  if( 0 < m_lst_DS.GetCount())
  {
    m_lst_DS.EnableWindow(true);
    m_btn_Connect_DS.EnableWindow(true);
    m_btn_Default_DS.EnableWindow(true);
    if(nDefault == -1)
    {
      m_lst_DS.SetCurSel(0);
    }
    OnLbnSelchangeDS();
  }
}

void CmfcDlgMain::OnLbnSelchangeDS()
{
  int           sel   = m_lst_DS.GetCurSel();
  TW_INT16      index = (TW_INT16)m_lst_DS.GetItemData(sel);
  pTW_IDENTITY  pID   = NULL;
  
  if(NULL != (pID = _pTWAINApp->getDataSource(index)) )
  {
    m_sStc_DS.Format( "Manufacturer:\t%s\r\n"
                      "Product Family:\t%s\r\n"
                      "Version:\t%d.%d\r\n"
                      "\t%s\r\n"
                      "TWAIN Protocol:\t%d.%d",
                      pID->Manufacturer, pID->ProductFamily, 
                      pID->Version.MajorNum, pID->Version.MinorNum, pID->Version.Info, 
                      pID->ProtocolMajor, pID->ProtocolMinor);
    UpdateData(false);
  }
}

void CmfcDlgMain::OnLbnDblclkDs()
{
  OnBnClickedConnectDs();
}

void CmfcDlgMain::OnBnClickedConnectDs()
{
  int           sel   = m_lst_DS.GetCurSel();
  TW_INT16      index = (TW_INT16)m_lst_DS.GetItemData(sel);
  pTW_IDENTITY  pID   = NULL;
  
  if(NULL != (pID = _pTWAINApp->getDataSource(index)) )
  {
    CmfcDlgConfigure dlg(this, pID->Id);
    dlg.DoModal();
  }
}

void CmfcDlgMain::OnBnClickedDefaultDs()
{
  int           sel   = m_lst_DS.GetCurSel();
  TW_INT16      index = (TW_INT16)m_lst_DS.GetItemData(sel);
  
  _pTWAINApp->connectDSM();
  if(3 == _pTWAINApp->m_DSMState)
  {
    _pTWAINApp->setDefaultDataSource(index);

    PopulateDSList();
    _pTWAINApp->disconnectDSM();
  }
}


UINT ClientThread(LPVOID pParam)
{
    Logger::Log("ClientThread started");
    CmfcDlgMain* pDlg = (CmfcDlgMain*)pParam;

    // 初始化socket
    SOCKET clientSocket = socket(AF_INET, SOCK_STREAM, 0);
    if (clientSocket == INVALID_SOCKET) {
        Logger::Log("Failed to create socket: %d", WSAGetLastError());
        return 1;
    }

    // 配置服务器地址和端口
    sockaddr_in serverAddr;
    serverAddr.sin_family = AF_INET;
    serverAddr.sin_port = htons(8080);  // 使用HTTP服务器端口
    inet_pton(AF_INET, "127.0.0.1", &serverAddr.sin_addr);  // 使用本地回环地址

    Logger::Log("Attempting to connect to server at 127.0.0.1:8080");

    // 连接到服务器
    if (connect(clientSocket, (sockaddr*)&serverAddr, sizeof(serverAddr)) == SOCKET_ERROR) {
        int error = WSAGetLastError();
        Logger::Log("Failed to connect to server: %d (Error description: %s)", 
                   error, GetWSAErrorString(error));
        closesocket(clientSocket);
        return 1;
    }

    Logger::Log("Successfully connected to server on port 8080");

    char recvBuffer[1024];
    while (true)
    {
        Logger::Log("Waiting for data...");
        // 接收数据
        int recvLen = recv(clientSocket, recvBuffer, sizeof(recvBuffer) - 1, 0);
        if (recvLen > 0)
        {
            recvBuffer[recvLen] = '\0';  // 确保接收到的数据是字符串
            Logger::Log("Received data: %s", recvBuffer);
            
            // 将接收到的数据通过PostMessage发送到主线程
            pDlg->PostMessage(WM_RECEIVE_DATA, (WPARAM)new CString(recvBuffer), 0);
        }
        else if (recvLen == 0)
        {
            Logger::Log("Connection closed by server");
            break;
        }
        else
        {
            int error = WSAGetLastError();
            Logger::Log("Error receiving data: %d (Error description: %s)", 
                       error, GetWSAErrorString(error));
            break;
        }
    }

    // 关闭socket
    closesocket(clientSocket);
    Logger::Log("ClientThread ended");
    return 0;
}

// 添加辅助函数来获取WSA错误描述
const char* GetWSAErrorString(int error)
{
    switch (error) {
        case WSAEADDRNOTAVAIL: return "Cannot assign requested address";
        case WSAECONNREFUSED: return "Connection refused";
        case WSAETIMEDOUT: return "Connection timed out";
        case WSAENETUNREACH: return "Network is unreachable";
        case WSAEHOSTUNREACH: return "No route to host";
        default: return "Unknown error";
    }
}

LRESULT CmfcDlgMain::OnReceiveData(WPARAM wParam, LPARAM lParam)
{
    Logger::Log("OnReceiveData");
    // 从wParam中获取接收到的数据
    CString* pReceivedData = (CString*)wParam;
    if (pReceivedData)
    {
        // 在UI上显示或处理接收到的数据
        m_sStc_DS = *pReceivedData;
        UpdateData(FALSE);  // 更新UI显示

        // 清理数据
        delete pReceivedData;
    }
    return 0;
}

// 简单的JSON解析函数，从JSON字符串中提取字段值
CString ExtractJsonValue(const CString& json, const CString& field) {
    int pos = json.Find(_T("\"") + field + _T("\":"));
    if (pos == -1) return _T("");
    
    pos += field.GetLength() + 3; // 跳过 "field":
    
    // 检查下一个字符
    TCHAR nextChar = json[pos];
    if (nextChar == _T('{')) {
        // 是一个对象，找到匹配的'}'
        int braceCount = 1;
        int startPos = pos;
        pos++;
        while (pos < json.GetLength() && braceCount > 0) {
            if (json[pos] == _T('{')) braceCount++;
            else if (json[pos] == _T('}')) braceCount--;
            pos++;
        }
        return json.Mid(startPos, pos - startPos);
    } else if (nextChar == _T('[')) {
        // 是一个数组，找到匹配的']'
        int bracketCount = 1;
        int startPos = pos;
        pos++;
        while (pos < json.GetLength() && bracketCount > 0) {
            if (json[pos] == _T('[')) bracketCount++;
            else if (json[pos] == _T(']')) bracketCount--;
            pos++;
        }
        return json.Mid(startPos, pos - startPos);
    } else if (nextChar == _T('"')) {
        // 是一个字符串，找到下一个非转义的引号
        pos++; // 跳过开始的引号
        int startPos = pos;
        bool escaped = false;
        while (pos < json.GetLength()) {
            if (json[pos] == _T('\\')) {
                escaped = !escaped;
            } else if (json[pos] == _T('"') && !escaped) {
                break;
            } else {
                escaped = false;
            }
            pos++;
        }
        return json.Mid(startPos, pos - startPos);
    } else {
        // 其他类型（数字、布尔值等），找到下一个逗号或大括号结束
        int startPos = pos;
        while (pos < json.GetLength() && json[pos] != _T(',') && json[pos] != _T('}')) {
            pos++;
        }
        return json.Mid(startPos, pos - startPos);
    }
}

// 从JSON对象中提取参数值
CString ExtractJsonParam(const CString& paramsJson, const CString& paramName) {
    CString searchKey = _T("\"") + paramName + _T("\":\"");
    int pos = paramsJson.Find(searchKey);
    if (pos == -1) return _T("");
    
    pos += searchKey.GetLength();
    int endPos = paramsJson.Find(_T("\""), pos);
    if (endPos == -1) return _T("");
    
    return paramsJson.Mid(pos, endPos - pos);
}

LRESULT CmfcDlgMain::OnHttpRequest(WPARAM wParam, LPARAM lParam)
{
    Logger::Log("OnHttpRequest: Main window received HTTP request message!");

    CString* pJsonRequest = (CString*)wParam;
    if (pJsonRequest)
    {
        Logger::Log("Received JSON request: %s", (LPCTSTR)*pJsonRequest);
        
        // 从JSON中提取URL、请求类型和参数
        CString url = ExtractJsonValue(*pJsonRequest, _T("url"));
        CString requestType = ExtractJsonValue(*pJsonRequest, _T("requestType"));
        CString paramsJson = ExtractJsonValue(*pJsonRequest, _T("params"));
        
        Logger::Log("Extracted URL: %s", (LPCTSTR)url);
        Logger::Log("Extracted RequestType: %s", (LPCTSTR)requestType);
        Logger::Log("Extracted Params: %s", (LPCTSTR)paramsJson);
        
        // 提取特定参数
        CString handleParam = ExtractJsonParam(paramsJson, _T("handle"));
        Logger::Log("Handle parameter: %s", (LPCTSTR)handleParam);
        
        // 处理请求
        bool isScannerListRequest = (requestType.CompareNoCase(_T("ScannerList")) == 0);
        bool handleScannersRequested = (handleParam.CompareNoCase(_T("scanners")) == 0);
        
        if (isScannerListRequest || handleScannersRequested) {
            Logger::Log("Detected scanner list request, returning scanner list");
            
            // 构建扫描仪列表
            CString scannerList = _T("SCANNERS:");
            
            // 只有在TWAIN已连接时才获取扫描仪列表
            if (_pTWAINApp && _pTWAINApp->m_DSMState >= 3) {
                // 获取扫描仪数量
                int scannerCount = 0;
                pTW_IDENTITY pID = NULL;
                
                while (NULL != (pID = _pTWAINApp->getDataSource((TW_INT16)scannerCount))) {
                    scannerList.AppendFormat(_T("[%d]%s;"), 
                                          scannerCount, 
                                          pID->ProductName);
                    scannerCount++;
                }
                
                if (scannerCount == 0) {
                    scannerList += _T("No scanners found");
                }
                
                Logger::Log("Found %d scanners", scannerCount);
            } else {
                scannerList += _T("DSM not connected");
                Logger::Log("DSM not connected, cannot get scanner list");
            }
            
            // 将扫描仪列表设置为状态显示
            m_sStc_DS = scannerList;
            
            // 保存扫描仪列表以便HTTP服务器返回给浏览器
            if (m_httpServer) {
                // 重新检查HttpServer实例和主窗口关联
                Logger::Log("HttpServer instance: %p", m_httpServer);
                HWND currentWnd = GetSafeHwnd();
                if (currentWnd) {
                    Logger::Log("Current main window handle: %p", currentWnd);
                    m_httpServer->SetMainWindow(currentWnd);
                    Logger::Log("Re-confirmed HTTP server main window handle: %p", currentWnd);
                    
                    // 重新设置窗口用户数据
                    ::SetWindowLongPtr(currentWnd, GWLP_USERDATA, (LONG_PTR)m_httpServer);
                    Logger::Log("Re-confirmed window user data pointing to HTTP server: %p", m_httpServer);
                }
                
                // 直接设置最后的响应
                m_httpServer->SetLastResponse(scannerList);
                Logger::Log("Scanner list saved for HTTP response: %s", (LPCTSTR)scannerList);
                
                // 保存一个全局副本作为备份
                AfxGetApp()->WriteProfileString(_T("Settings"), _T("LastScannerList"), scannerList);
                Logger::Log("Saved scanner list to application profile as backup");
            } else {
                Logger::Log("ERROR: m_httpServer is NULL, cannot set last response");
            }
        } else {
            // 显示JSON请求内容
            m_sStc_DS = *pJsonRequest;
        }
        
        UpdateData(FALSE);  // 更新UI显示

        // 记录日志
        CString strLog;
        strLog.Format(_T("Received HTTP request content: %s"), *pJsonRequest);
        Logger::Log((LPCTSTR)strLog);

        // 记录到文件日志
        Logger::Log("HTTP request successfully processed by main window");

        // 清理数据
        delete pJsonRequest;
        Logger::Log("HTTP request processing completed");
    }
    else
    {
        Logger::Log("OnHttpRequest: Received NULL request pointer");
    }
    
    return 0;
}

LRESULT CmfcDlgMain::OnTcpData(WPARAM wParam, LPARAM lParam)
{
    // Implementation of OnTcpData method
    return 0;
}

LRESULT CmfcDlgMain::OnConnectScanner(WPARAM wParam, LPARAM lParam)
{
    Logger::Log("OnConnectScanner: Received request to connect to scanner");
    
    // 从wParam参数中获取扫描仪名称
    HGLOBAL hMem = (HGLOBAL)wParam;
    if (!hMem)
    {
        Logger::Log("ERROR: Invalid memory handle for scanner name");
        return 1;
    }
    
    char* pScannerName = (char*)::GlobalLock(hMem);
    if (!pScannerName)
    {
        Logger::Log("ERROR: Failed to lock memory for scanner name");
        ::GlobalFree(hMem);
        return 1;
    }
    
    // 将扫描仪名称转换为CString
    CString scannerName;
#ifdef _UNICODE
    // 将UTF-8转换为Unicode
    int reqLen = MultiByteToWideChar(CP_UTF8, 0, pScannerName, -1, NULL, 0);
    if (reqLen > 0)
    {
        wchar_t* wBuffer = new wchar_t[reqLen];
        if (MultiByteToWideChar(CP_UTF8, 0, pScannerName, -1, wBuffer, reqLen))
        {
            scannerName = wBuffer;
        }
        delete[] wBuffer;
    }
#else
    scannerName = pScannerName;
#endif
    
    // 释放全局内存
    ::GlobalUnlock(hMem);
    ::GlobalFree(hMem);
    
    Logger::Log("Connecting to scanner: %s", (LPCTSTR)scannerName);
    
    // 检查TWAIN应用实例是否有效
    if (!_pTWAINApp)
    {
        Logger::Log("ERROR: TWAIN application instance is NULL");
        return 1;
    }
    
    // 确保DSM已连接
    if (_pTWAINApp->m_DSMState < 3)
    {
        Logger::Log("Connecting to DSM...");
        _pTWAINApp->connectDSM();
        
        // 检查连接后的状态
        if (_pTWAINApp->m_DSMState < 3)
        {
            Logger::Log("ERROR: Failed to connect to DSM");
            return 1;
        }
    }
    
    // 查找匹配扫描仪名称的设备
    int scannerIndex = -1;
    pTW_IDENTITY pID = NULL;
    int scannerCount = 0;
    
    while (NULL != (pID = _pTWAINApp->getDataSource((TW_INT16)scannerCount)))
    {
        CString currentName(pID->ProductName);
        Logger::Log("Checking scanner [%d]: %s", scannerCount, (LPCTSTR)currentName);
        
        if (currentName.CompareNoCase(scannerName) == 0)
        {
            scannerIndex = scannerCount;
            Logger::Log("Found matching scanner at index %d", scannerIndex);
            break;
        }
        
        scannerCount++;
    }
    
    // 如果找到匹配的扫描仪
    if (scannerIndex >= 0)
    {
        Logger::Log("Attempting to connect to scanner at index %d", scannerIndex);
        
        // 如果当前已连接扫描仪，先断开
        if (_pTWAINApp->m_DSMState > 3)
        {
            Logger::Log("Disconnecting from current DS...");
            _pTWAINApp->unloadDS();
        }
        
        // 连接到选定的扫描仪
        _pTWAINApp->loadDS((TW_INT16)scannerIndex);
        
        // 检查连接是否成功(loadDS成功会将状态设置为4)
        if (_pTWAINApp->m_DSMState >= 4)
        {
            Logger::Log("Successfully connected to scanner: %s", (LPCTSTR)scannerName);
            
            // 更新UI显示
            m_sStc_DS.Format(_T("Connected to scanner: %s"), scannerName);
            UpdateData(FALSE);
            
            // 刷新界面状态
            m_btn_Connect_DS.EnableWindow(FALSE);
            
            return 0; // 成功
        }
        else
        {
            Logger::Log("ERROR: Failed to connect to scanner at index %d", scannerIndex);
            m_sStc_DS.Format(_T("Failed to connect to scanner: %s"), scannerName);
            UpdateData(FALSE);
        }
    }
    else
    {
        Logger::Log("ERROR: Scanner not found with name: %s", (LPCTSTR)scannerName);
        m_sStc_DS.Format(_T("Scanner not found: %s"), scannerName);
        UpdateData(FALSE);
    }
    
    return 1; // 失败
}

