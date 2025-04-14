/***************************************************************************
* Copyright � 2007 TWAIN Working Group:  
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
#include "http_server.h"
#include <fstream>
#include <ctime>

#include "twain.h"
#include "..\src\twainapp.h"
#include "..\src\dsminterface.h"
#include "TwainString.h"
#include ".\TW_Enum_Dlg.h"

#ifdef _DEBUG
#define new DEBUG_NEW
#endif



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
  , m_httpServer(new HttpServer())  // 创建 HTTP 服务器实例
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
  //}}AFX_MSG_MAP
  ON_WM_DESTROY()
  ON_LBN_SELCHANGE(IDL_DS, OnLbnSelchangeDS)
  ON_LBN_DBLCLK(IDL_DS, OnLbnDblclkDs)
  ON_BN_CLICKED(IDB_CONNECT_DS, OnBnClickedConnectDs)
  ON_BN_CLICKED(IDB_DEFAULT_DS, OnBnClickedDefaultDs)
END_MESSAGE_MAP()


// CmfcDlgMain message handlers

BOOL CmfcDlgMain::OnInitDialog()
{
    try {
        // 添加日志初始化代码 - 在最开始的位置
        char exePath[MAX_PATH];
        if (GetModuleFileNameA(NULL, exePath, MAX_PATH) == 0) {
            OutputDebugStringA("Failed to get module filename\n");
            return FALSE;
        }
        
        std::string exeDir = std::string(exePath);
        size_t lastSlash = exeDir.find_last_of("\\/");
        if (lastSlash == std::string::npos) {
            OutputDebugStringA("Failed to find directory separator in path\n");
            return FALSE;
        }
        exeDir = exeDir.substr(0, lastSlash);
        
        // 设置日志目录为程序目录下的 logs 文件夹
        std::string logDir = exeDir + "\\logs";
        Logger::SetLogDirectory(logDir.c_str());
        Logger::Init("aaaaaaa.log");
        Logger::Log("=== Application Started ===");

        CDialog::OnInitDialog();

        // Set the icon for this dialog
        SetIcon(m_hIcon, TRUE);     // Set big icon
        SetIcon(m_hIcon, FALSE);    // Set small icon

        // 初始化 TWAIN 应用程序
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

        // 连接到 DSM
        Logger::Log("Attempting to connect to DSM...");
        _pTWAINApp->connectDSM();
        if (_pTWAINApp->m_DSMState < 3) {
            Logger::Log("Failed to connect to DSM, state: %d", _pTWAINApp->m_DSMState);
            return FALSE;
        }
        Logger::Log("Successfully connected to DSM");

        // 填充扫描仪列表
        Logger::Log("Populating scanner list...");
        PopulateDSList();
        Logger::Log("Scanner list populated");

        // 创建并初始化 HTTP 服务器
        m_httpServer = new HttpServer();
        if (!m_httpServer) {
            Logger::Log("Failed to create HTTP server instance");
            return FALSE;
        }

        // 设置 TWAIN 应用实例到 HTTP 服务器
        m_httpServer->SetTwainApp(_pTWAINApp);
        Logger::Log("TWAIN application instance set to HTTP server");

        // 启动 HTTP 服务器
        if (!m_httpServer->Start(8080)) {
            Logger::Log("Failed to start HTTP server");
            MessageBox(_T("Failed to start HTTP server. Please check if port 8080 is available."), 
                      _T("Server Error"), MB_ICONERROR);
            return FALSE;
        }
        Logger::Log("HTTP server started successfully on port 8080");

        // 更新状态显示
        CString status;
        status.Format(_T("HTTP server running on port 8080"));
        SetDlgItemText(IDC_STC_DS, status);

        // 初始化完成
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
    try {
        Logger::Log("=== Application Shutting Down ===");
        
        // 停止 HTTP 服务器
        if (m_httpServer) {
            m_httpServer->Stop();
            delete m_httpServer;
            m_httpServer = nullptr;
            Logger::Log("HTTP server stopped and cleaned up");
        }

        // 清理 TWAIN 应用程序
        if(_pTWAINApp)
        {
            if (_pTWAINApp->m_DSMState >= 3) {
                _pTWAINApp->disconnectDSM();
                Logger::Log("Disconnected from DSM");
            }
            Logger::Log("Cleaning up TWAIN application");
            _pTWAINApp->exit();
            delete _pTWAINApp;
            _pTWAINApp = NULL;
        }

        CDialog::OnDestroy();
        Logger::Log("=== Application Ended ===");
        Logger::Cleanup();
    }
    catch (const std::exception& e) {
        OutputDebugStringA(e.what());
    }
    catch (...) {
        OutputDebugStringA("Unknown exception in OnDestroy");
    }
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
