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
* @file mfcDlgMain.h
* Header file for the main dialog for the MFC TWAIN App Sample application
* @author JFL Peripheral Solutions Inc.
* @date April 2007
*/

#pragma once

// 定义自定义消息
#define WM_RECEIVE_DATA (WM_USER + 100)
#define WM_CONNECT_SCANNER (WM_USER + 102)

// 声明全局的客户端线程函数
UINT WINAPI ClientThread(LPVOID pParam);

// 禁用inet_addr的废弃警告
#define _WINSOCK_DEPRECATED_NO_WARNINGS

#ifndef __AFXWIN_H__
  #error include 'stdafx.h' before including this file for PCH
#endif

#include "CommonTWAIN.h"
#include "afxwin.h"
#include "http_server.h"  // 添加 HTTP 服务器头文件

//forward declaration for class pointer
class TwainApp;

/**
* Main dialalog window for applicaiton
*/
class CmfcDlgMain : public CDialog
{
// Construction
public:
/**
* standard constructor
*/
  CmfcDlgMain(CWnd* pParent = NULL);

// Dialog Data
  enum { IDD = IDD_MFC32_APP };

  protected:
  virtual void DoDataExchange(CDataExchange* pDX);// DDX/DDV support


// Implementation
protected:
  HICON     m_hIcon;
  TwainApp *_pTWAINApp;
  HttpServer* m_httpServer;  // 指向HTTP服务器的指针而不是对象

  // Generated message map functions
  virtual BOOL OnInitDialog();
  afx_msg void OnSysCommand(UINT nID, LPARAM lParam);
  afx_msg void OnPaint();
  afx_msg HCURSOR OnQueryDragIcon();
  DECLARE_MESSAGE_MAP()
  void PopulateDSList();
  afx_msg LRESULT OnHttpRequest(WPARAM wParam, LPARAM lParam);
  afx_msg LRESULT OnHttpServerStarted(WPARAM wParam, LPARAM lParam);
  afx_msg LRESULT OnHttpServerError(WPARAM wParam, LPARAM lParam);
  afx_msg LRESULT OnTcpData(WPARAM wParam, LPARAM lParam);
  afx_msg void OnDestroy();
  afx_msg void OnLbnSelchangeDS();
  afx_msg void OnBnClickedConnectDs();
  afx_msg void OnLbnDblclkDs();
  afx_msg void OnBnClickedDefaultDs();
  afx_msg LRESULT OnReceiveData(WPARAM wParam, LPARAM lParam);
  afx_msg LRESULT OnConnectScanner(WPARAM wParam, LPARAM lParam);

public:
  CString   m_sStc_DS;
  CListBox  m_lst_DS;
  CButton   m_btn_Connect_DS;
  CButton   m_btn_Default_DS;
};
