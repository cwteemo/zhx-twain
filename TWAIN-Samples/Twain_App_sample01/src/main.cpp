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
* @file main.cpp
* The entry point to launch the application
* @author TWAIN Working Group
* @date October 2007
 */

#ifdef HAVE_CONFIG_H
#include <config.h>
#endif

#include "main.h"

// I found that compiling using the sunfreeware.com stuff on Solaris 9
// required this typedef. This is related to the inclusion of signal.h
#if defined (__SVR4) && defined (__sun)
typedef union {
  long double  _q;
  uint32_t     _l[4];
} upad128_t;
#endif

#include <signal.h>

// Windows平台需要的头文件
#ifdef TWH_CMP_MSC
#include <windows.h>
#endif

#include "CommonTWAIN.h"
#include "TwainAppCMD.h"
#include "TwainApp_ui.h"
#include "Logger.h"
#ifdef TWH_CMP_MSC
#include "http_server.h"
#endif

using namespace std;

//////////////////////////////////////////////////////////////////////////////
// Global Variables
TwainAppCMD  *gpTwainApplicationCMD;  /**< The main application */
extern bool   gUSE_CALLBACKS;         // defined in TwainApp.cpp

//////////////////////////////////////////////////////////////////////////////
/** 
* Display exit message.
* @param[in] _sig not used.
*/
void onSigINT(int _sig)
{
  UNUSEDARG(_sig);
  cout << "\nGoodbye!" << endl;
  exit(0);
}

//////////////////////////////////////////////////////////////////////////////
/** 
* Negotiate a capabilities between the app and the DS
* @param[in] _pCap the capabilities to negotiate
*/
void negotiate_CAP(const pTW_CAPABILITY _pCap)
{
  string input;

  // -Setting one cap could change another cap so always refresh the caps
  // before working with another one.  
  // -Another method of doing this is to let the DS worry about the state
  // of the caps instead of keeping a copy in the app like I'm doing.
  gpTwainApplicationCMD->initCaps();

  for (;;)
  {
    if((TWON_ENUMERATION == _pCap->ConType) || 
       (TWON_ONEVALUE == _pCap->ConType))
    {
      TW_MEMREF pVal = _DSM_LockMemory(_pCap->hContainer);

      // print the caps current value
      if(TWON_ENUMERATION == _pCap->ConType)
      {
        print_ICAP(_pCap->Cap, (pTW_ENUMERATION)(pVal));
      }
      else // TWON_ONEVALUE
      {
        print_ICAP(_pCap->Cap, (pTW_ONEVALUE)(pVal));
      }

      cout << "\nset cap# > ";
      cin >> input;
      cout << endl;

      if("q" == input)
      {
        _DSM_UnlockMemory(_pCap->hContainer);
        break;
      }
      else
      {
        int n = atoi(input.c_str());
        TW_UINT16  valUInt16 = 0;
		pTW_FIX32  pValFix32 = {0};
		pTW_FRAME  pValFrame = {0};

        // print the caps current value
        if(TWON_ENUMERATION == _pCap->ConType)
        {
          switch(((pTW_ENUMERATION)pVal)->ItemType)
          {
            case TWTY_UINT16:
              valUInt16 = ((pTW_UINT16)(&((pTW_ENUMERATION)pVal)->ItemList))[n];
            break;

            case TWTY_FIX32:
              pValFix32 = &((pTW_ENUMERATION_FIX32)pVal)->ItemList[n];
            break;
            
            case TWTY_FRAME:
              pValFrame = &((pTW_ENUMERATION_FRAME)pVal)->ItemList[n];
            break;
          }

          switch(_pCap->Cap)
          {
            case ICAP_PIXELTYPE:
              gpTwainApplicationCMD->set_ICAP_PIXELTYPE(valUInt16);
            break;

            case ICAP_BITDEPTH:
              gpTwainApplicationCMD->set_ICAP_BITDEPTH(valUInt16);
            break;

            case ICAP_UNITS:
              gpTwainApplicationCMD->set_ICAP_UNITS(valUInt16);
            break;
            
            case ICAP_XFERMECH:
              gpTwainApplicationCMD->set_ICAP_XFERMECH(valUInt16);
            break;
          
            case ICAP_XRESOLUTION:
            case ICAP_YRESOLUTION:
              gpTwainApplicationCMD->set_ICAP_RESOLUTION(_pCap->Cap, pValFix32);
            break;

            case ICAP_FRAMES:
              gpTwainApplicationCMD->set_ICAP_FRAMES(pValFrame);
            break;

            default:
              cerr << "Unsupported capability" << endl;
            break;
          }
        }
      }
      _DSM_UnlockMemory(_pCap->hContainer);
    }
    else
    {
      cerr << "Unsupported capability" << endl;
      break;
    }
  }

  return;
}

//////////////////////////////////////////////////////////////////////////////
/**
* drives main capabilities menu.  Negotiate all capabilities
*/
void negotiateCaps()
{
  // If the app is not in state 4, don't even bother showing this menu.
  if(gpTwainApplicationCMD->m_DSMState < 4)
  {
    cerr << "\nNeed to open a source first\n" << endl;
    return;
  }

  string input;

  // Loop forever until either SIGINT is heard or user types done to go back
  // to the main menu.
  for (;;)
  {
    printMainCaps();
    cout << "\n(h for help) > ";
    cin >> input;
    cout << endl;

    if("q" == input)
    {
      break;
    }
    else if("h" == input)
    {
      printMainCaps();
    }
    else if("1" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_XFERMECH));
    }
    else if("2" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_PIXELTYPE));
    }
    else if("3" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_BITDEPTH));
    }
    else if("4" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_XRESOLUTION));
    }
    else if("5" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_YRESOLUTION));
    }
    else if("6" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_FRAMES));
    }
    else if("7" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_UNITS));
    }
    else
    {
      printMainCaps();
    }
  }

  return;
}

//////////////////////////////////////////////////////////////////////////////
/**
* Enables the source. The source will let us know when it is ready to scan by
* calling our registered callback function.
*/
// 旧版本的 EnableDS，保留用于向后兼容，但已移除消息循环
void EnableDS()
{
  gpTwainApplicationCMD->m_DSMessage = 0;
  #ifdef TWNDS_OS_LINUX

    int test;
    sem_getvalue(&(gpTwainApplicationCMD->m_TwainEvent), &test);
    while(test<0)
    {
      sem_post(&(gpTwainApplicationCMD->m_TwainEvent));    // Event semaphore Handle
      sem_getvalue(&(gpTwainApplicationCMD->m_TwainEvent), &test);
    }
    while(test>0)
    {
      sem_wait(&(gpTwainApplicationCMD->m_TwainEvent)); // event semaphore handle
      sem_getvalue(&(gpTwainApplicationCMD->m_TwainEvent), &test);
    }

  #endif
  // -Enable the data source. This puts us in state 5 which means that we
  // have to wait for the data source to tell us to move to state 6 and
  // start the transfer.  Once in state 5, no more set ops can be done on the
  // caps, only get ops.
  // -The scan will not start until the source calls the callback function
  // that was registered earlier.
  // NOTE: 消息循环已移除，由Go端实现
#ifdef TWNDS_OS_WIN
  // 使用与初始化时相同的窗口获取逻辑（与 zhx_twain() 中一致）
  // 先尝试 GetConsoleWindow()，失败则使用 GetDesktopWindow()
  HWND hWnd = GetConsoleWindow();
  if(!hWnd)
  {
    Logger::Log("EnableDS: No console window, using GetDesktopWindow()");
    hWnd = GetDesktopWindow();
  }
  else
  {
    Logger::Log("EnableDS: Using console window: %p", (void*)hWnd);
  }
  
  if(!hWnd)
  {
    Logger::Log("EnableDS: ERROR - Could not get valid window handle!");
    return;
  }
  
  Logger::Log("EnableDS: About to call enableDS with window handle: %p", (void*)hWnd);
  if(!gpTwainApplicationCMD->enableDS(hWnd, FALSE))
  {
    Logger::Log("EnableDS: enableDS() returned false, exiting");
    return;
  }
  Logger::Log("EnableDS: enableDS() returned true - message loop should be handled by Go side");
#else
  if(!gpTwainApplicationCMD->enableDS(0, TRUE))
  {
    return;
  }
#endif

  // 消息循环已移除，由Go端实现
  // Go端需要调用 zhx_ProcessEvent() 来处理消息
  return;
}

//////////////////////////////////////////////////////////////////////////////
/**
* Callback funtion for DS.  This is a callback function that will be called by
* the source when it is ready for the application to start a scan. This 
* callback needs to be registered with the DSM before it can be called.
* It is important that the application returns right away after recieving this
* message.  Set a flag and return.  Do not process the callback in this function.
*/
#ifdef TWH_CMP_MSC
TW_UINT16 FAR PASCAL
#else
FAR PASCAL TW_UINT16 
#endif
DSMCallback(pTW_IDENTITY _pOrigin,
            pTW_IDENTITY _pDest,
            TW_UINT32    _DG,
            TW_UINT16    _DAT,
            TW_UINT16    _MSG,
            TW_MEMREF    _pData)
{
  UNUSEDARG(_pDest);
  UNUSEDARG(_DG);
  UNUSEDARG(_DAT);
  UNUSEDARG(_pData);

  TW_UINT16 twrc = TWRC_SUCCESS;

  // we are only waiting for callbacks from our datasource, so validate
  // that the originator.
  if(0 == _pOrigin ||
     _pOrigin->Id != gpTwainApplicationCMD->getDataSource()->Id)
  {
    return TWRC_FAILURE;
  }
  switch (_MSG)
  {
    case MSG_XFERREADY:
    case MSG_CLOSEDSREQ:
    case MSG_CLOSEDSOK:
    case MSG_NULL:
      gpTwainApplicationCMD->m_DSMessage = _MSG;
      // now signal the event semaphore
    #ifdef TWNDS_OS_LINUX
      {
      int test=12345;
      sem_post(&(gpTwainApplicationCMD->m_TwainEvent));    // Event semaphore Handle
  }
    #endif
      break;

    default:
      cerr << "Error - Unknown message in callback routine" << endl;
      twrc = TWRC_FAILURE;
      break;
  }

  return twrc;
}


//////////////////////////////////////////////////////////////////////////////
/**
* main program loop
*/
#ifdef TWH_CMP_MSC
int _tmain(int argc, _TCHAR* argv[])
#else
int main(int argc, char *argv[])
#endif
{
  UNUSEDARG(argc);
  UNUSEDARG(argv);
  int ret = EXIT_SUCCESS;
  return ret;
  Logger::Init();  // 初始化日志

  // Instantiate the TWAIN application CMD class
  HWND parentWindow = NULL;

#ifdef TWH_CMP_MSC
  parentWindow = GetConsoleWindow();
#endif
  gpTwainApplicationCMD = new TwainAppCMD(parentWindow);

  // setup a signal handler for SIGINT that will allow the program to stop
  signal(SIGINT, &onSigINT);

  string input;
  return ret;
  printOptions();

  // start the main event loop
  for (;;)
  {
    cout << "\n(h for help) > ";
    cin >> input;
    cout << endl;


    if("q" == input)
    {
      break;
    }
    else if("h" == input)
    {
      printOptions();
    }
    else if("cdsm" == input)
    {
      gpTwainApplicationCMD->connectDSM();
    }
    else if("xdsm" == input)
    {
      gpTwainApplicationCMD->disconnectDSM();
    }
    else if("lds" == input)
    {
      gpTwainApplicationCMD->printAvailableDataSources();
    }
    else if("pds" == input.substr(0,3))
    {
      gpTwainApplicationCMD->printIdentityStruct(atoi(input.substr(3,input.length()-3).c_str()));
    }
    else if("cds" == input.substr(0,3))
    {
      gpTwainApplicationCMD->loadDS(atoi(input.substr(3,input.length()-3).c_str()));
    }
    else if("xds" == input)
    {
      gpTwainApplicationCMD->unloadDS();
    }
    else if("caps" == input)
    {
      if(gpTwainApplicationCMD->m_DSMState < 3)
      {
        cout << "\nYou need to select a source first!" << endl;
      }
      else
      {
        negotiateCaps();
        printOptions();
      }
    }
    else if("scan" == input)
    {
      EnableDS();
    }
    else
    {
      // default action
      printOptions();
    }
  }

  gpTwainApplicationCMD->exit();
  delete gpTwainApplicationCMD;
  gpTwainApplicationCMD = 0;

  Logger::Cleanup();  // 清理日志
  return ret;
}


/**
 * 测试DLL接口是否可以被调用
 */
void zhx_twain_test() {
    
    std::cout << "Hello, World!\nThis message comes from a CPP function!" << std::endl;
    
    // 返回测试成功的字符串
}

void zhx_twain() {
    std::cout << "Hello, World!\nThis message comes from a CPP function!" << std::endl;
    int ret = EXIT_SUCCESS;
    Logger::Init();  // 初始化日志
    HWND parentWindow = NULL;
    std::cout << "A" << std::endl;
    Logger::Log("A");
    #ifdef TWH_CMP_MSC
    // 尝试获取控制台窗口（与原始项目一致）
    parentWindow = GetConsoleWindow();
    if(!parentWindow)
    {
        // 如果没有控制台窗口，尝试使用桌面窗口
        Logger::Log("No console window found, using desktop window");
        parentWindow = GetDesktopWindow();
    }
    else
    {
        Logger::Log("Using console window: %p", (void*)parentWindow);
    }
    #endif
    std::cout << "B" << std::endl;
    Logger::Log("B");
    gpTwainApplicationCMD = new TwainAppCMD(parentWindow);
    std::cout << "C" << std::endl;
    Logger::Log("C");
    gpTwainApplicationCMD->connectDSM();
    std::cout << "D" << std::endl;
    Logger::Log("D");
    //gpTwainApplicationCMD->disconnectDSM();
    std::cout << "E" << std::endl;
    Logger::Log("E");
    gpTwainApplicationCMD->printAvailableDataSources();
    std::cout << "F" << std::endl;
    Logger::Log("F");
    //gpTwainApplicationCMD->printIdentityStruct(atoi("2"));
    std::cout << "G" << std::endl;
    Logger::Log("G");
    //测试 写死的需要连接的扫描仪
    gpTwainApplicationCMD->loadDS(atoi("1"));
    std::cout << "H" << std::endl;
    Logger::Log("H");
    //gpTwainApplicationCMD->unloadDS();
    std::cout << "I" << std::endl;
    Logger::Log("I");
    EnableDS();
    std::cout << "J" << std::endl;
    Logger::Log("J");
    gpTwainApplicationCMD->exit();
    std::cout << "K" << std::endl;
    Logger::Log("K");
    delete gpTwainApplicationCMD;
    std::cout << "L" << std::endl;
    Logger::Log("L");
    gpTwainApplicationCMD = 0;
    std::cout << "M" << std::endl;
    Logger::Log("M");
    Logger::Cleanup();  // 清理日志
    std::cout << "N" << std::endl;
    Logger::Log("N");
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 处理单个Windows消息（供Go端调用）
 * @param msg 指向MSG结构的指针
 * @return 1表示需要继续消息循环，0表示可以退出循环
 */
extern "C" __declspec(dllexport) int zhx_ProcessEvent(MSG* msg)
{
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_ProcessEvent: ERROR - gpTwainApplicationCMD is NULL!");
        return 0;
    }

    if(!msg)
    {
        Logger::Log("zhx_ProcessEvent: ERROR - msg is NULL!");
        return 0;
    }

    // 检查是否已有DS消息
    if(gpTwainApplicationCMD->m_DSMessage)
    {
        Logger::Log("zhx_ProcessEvent: DS message already received: %d", gpTwainApplicationCMD->m_DSMessage);
        return 0;  // 已有消息，可以退出循环
    }

    // 处理WM_QUIT消息
    if(msg->message == WM_QUIT)
    {
        Logger::Log("zhx_ProcessEvent: Received WM_QUIT");
        return 0;  // 退出循环
    }

    // 如果使用回调，只需要移除消息，不需要处理TWAIN事件
    if(gUSE_CALLBACKS)
    {
        // 回调模式下，只移除消息，不处理
        return 1;  // 继续循环，等待回调
    }

    // 非回调模式：需要调用 MSG_PROCESSEVENT
    if(!gpTwainApplicationCMD->getAppIdentity() || !gpTwainApplicationCMD->getDataSource())
    {
        Logger::Log("zhx_ProcessEvent: Invalid pointers");
        return 1;  // 继续循环
    }

    TW_EVENT twEvent = {0};
    twEvent.pEvent = (TW_MEMREF)msg;
    twEvent.TWMessage = MSG_NULL;
    
    TW_UINT16 twRC = _DSM_Entry(
        gpTwainApplicationCMD->getAppIdentity(),
        gpTwainApplicationCMD->getDataSource(),
        DG_CONTROL,
        DAT_EVENT,
        MSG_PROCESSEVENT,
        (TW_MEMREF)&twEvent);

    // 处理TWAIN事件
    if(twRC == TWRC_DSEVENT)
    {
        switch (twEvent.TWMessage)
        {
            case MSG_XFERREADY:
            case MSG_CLOSEDSREQ:
            case MSG_CLOSEDSOK:
            case MSG_NULL:
                gpTwainApplicationCMD->m_DSMessage = twEvent.TWMessage;
                Logger::Log("zhx_ProcessEvent: TWAIN event received: %d", twEvent.TWMessage);
                return 0;  // 收到TWAIN消息，可以退出循环
            default:
                Logger::Log("zhx_ProcessEvent: Unknown TWAIN message: %d", twEvent.TWMessage);
                break;
        }
    }
    
    // 如果不是TWAIN事件，需要分发消息
    if(twRC != TWRC_DSEVENT)
    {
        if(msg->hwnd != NULL)
        {
            TranslateMessage(msg);
            DispatchMessage(msg);
        }
    }

    return 1;  // 继续循环
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 获取当前的DS消息状态
 * @return DS消息值，0表示还没有消息
 */
extern "C" __declspec(dllexport) int zhx_GetDSMessage()
{
    if(!gpTwainApplicationCMD)
    {
        return 0;
    }
    return (int)gpTwainApplicationCMD->m_DSMessage;
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 启用DS（供Go端调用，不进入消息循环）
 * @param hWnd 窗口句柄
 * @return 1表示成功，0表示失败
 */
//////////////////////////////////////////////////////////////////////////////
/**
 * 初始化TWAIN环境（供Go端调用）
 * @param hWnd 父窗口句柄，可以为NULL
 * @return 1表示成功，0表示失败
 */
extern "C" __declspec(dllexport) int zhx_Init(HWND hWnd)
{
    if(gpTwainApplicationCMD)
    {
        Logger::Log("zhx_Init: WARNING - gpTwainApplicationCMD already exists, cleaning up first");
        delete gpTwainApplicationCMD;
        gpTwainApplicationCMD = 0;
    }

    Logger::Init();  // 初始化日志

    if(!hWnd)
    {
        #ifdef TWH_CMP_MSC
        hWnd = GetConsoleWindow();
        if(!hWnd)
        {
            Logger::Log("zhx_Init: No console window, using GetDesktopWindow()");
            hWnd = GetDesktopWindow();
        }
        else
        {
            Logger::Log("zhx_Init: Using console window: %p", (void*)hWnd);
        }
        #endif
    }

    Logger::Log("zhx_Init: Creating TwainAppCMD with window handle: %p", (void*)hWnd);
    gpTwainApplicationCMD = new TwainAppCMD(hWnd);
    
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_Init: ERROR - Failed to create TwainAppCMD!");
        return 0;
    }

    Logger::Log("zhx_Init: Connecting to DSM...");
    gpTwainApplicationCMD->connectDSM();
    
    Logger::Log("zhx_Init: Successfully initialized");
    return 1;
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 加载数据源（供Go端调用）
 * @param deviceId 设备ID（从1开始）
 * @return 1表示成功，0表示失败
 */
extern "C" __declspec(dllexport) int zhx_LoadDS(int deviceId)
{
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_LoadDS: ERROR - gpTwainApplicationCMD is NULL! Call zhx_Init first.");
        return 0;
    }

    Logger::Log("zhx_LoadDS: Loading device with ID: %d", deviceId);
    gpTwainApplicationCMD->loadDS(deviceId);
    
    Logger::Log("zhx_LoadDS: Device loaded");
    return 1;
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 清理TWAIN环境（供Go端调用）
 * @return 1表示成功，0表示失败
 */
extern "C" __declspec(dllexport) int zhx_Cleanup()
{
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_Cleanup: WARNING - gpTwainApplicationCMD is NULL");
        return 0;
    }

    Logger::Log("zhx_Cleanup: Exiting TWAIN...");
    gpTwainApplicationCMD->exit();
    
    Logger::Log("zhx_Cleanup: Deleting TwainAppCMD...");
    delete gpTwainApplicationCMD;
    gpTwainApplicationCMD = 0;
    
    Logger::Log("zhx_Cleanup: Cleaning up logger...");
    Logger::Cleanup();
    
    Logger::Log("zhx_Cleanup: Successfully cleaned up");
    return 1;
}

extern "C" __declspec(dllexport) int zhx_EnableDS(HWND hWnd)
{
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_EnableDS: ERROR - gpTwainApplicationCMD is NULL! Call zhx_Init first.");
        return 0;
    }

    // 重置DS消息
    gpTwainApplicationCMD->m_DSMessage = 0;

    if(!hWnd)
    {
        Logger::Log("zhx_EnableDS: No window handle provided, trying to get one");
        #ifdef TWH_CMP_MSC
        hWnd = GetConsoleWindow();
        if(!hWnd)
        {
            hWnd = GetDesktopWindow();
        }
        #endif
    }

    if(!hWnd)
    {
        Logger::Log("zhx_EnableDS: ERROR - Could not get valid window handle!");
        return 0;
    }

    // 验证窗口句柄是否有效
    #ifdef TWH_CMP_MSC
    if(!IsWindow(hWnd))
    {
        Logger::Log("zhx_EnableDS: ERROR - Invalid window handle: %p", (void*)hWnd);
        return 0;
    }
    #endif

    Logger::Log("zhx_EnableDS: Calling enableDS with window handle: %p", (void*)hWnd);
    if(!gpTwainApplicationCMD->enableDS(hWnd, FALSE))
    {
        Logger::Log("zhx_EnableDS: enableDS() returned false");
        return 0;
    }

    Logger::Log("zhx_EnableDS: enableDS() returned true");
    
    // 在返回前处理一些消息，确保TWAIN消息能够被及时处理
    // 这很重要，因为DSM_Entry可能会立即发送消息
    #ifdef TWH_CMP_MSC
    MSG msg;
    int processed = 0;
    while(PeekMessage(&msg, NULL, 0, 0, PM_REMOVE) && processed < 10)
    {
        TranslateMessage(&msg);
        DispatchMessage(&msg);
        processed++;
    }
    if(processed > 0)
    {
        Logger::Log("zhx_EnableDS: Processed %d messages after enableDS", processed);
    }
    #endif
    
    return 1;  // 成功
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 处理扫描完成后的操作（供Go端调用）
 * @return 1表示成功，0表示失败
 */
extern "C" __declspec(dllexport) int zhx_HandleScanReady()
{
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_HandleScanReady: ERROR - gpTwainApplicationCMD is NULL!");
        return 0;
    }

    // 检查是否有XFERREADY消息
    if(gpTwainApplicationCMD->m_DSMessage == MSG_XFERREADY)
    {
        // 移动到状态6并开始扫描
        gpTwainApplicationCMD->m_DSMState = 6;
        gpTwainApplicationCMD->startScan();
        
        // 扫描完成后，禁用DS
        gpTwainApplicationCMD->disableDS();
        
        Logger::Log("zhx_HandleScanReady: Scan completed");
        return 1;
    }

    Logger::Log("zhx_HandleScanReady: No XFERREADY message");
    return 0;
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 获取设备列表（供HTTP服务器使用）
 * @param deviceList 输出缓冲区，存储JSON格式的设备列表
 * @param bufferSize 缓冲区大小
 * @return 1表示成功，0表示失败
 */
extern "C" __declspec(dllexport) int zhx_GetDevicesList(char* deviceList, int bufferSize)
{
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_GetDevicesList: ERROR - gpTwainApplicationCMD is NULL!");
        if(deviceList && bufferSize > 0)
        {
            strncpy(deviceList, "{\"devices\":[],\"count\":0}", bufferSize - 1);
            deviceList[bufferSize - 1] = '\0';
        }
        return 0;
    }

    // 确保DSM已连接
    if(gpTwainApplicationCMD->m_DSMState < 3)
    {
        Logger::Log("zhx_GetDevicesList: Connecting to DSM...");
        gpTwainApplicationCMD->connectDSM();
    }

    if(gpTwainApplicationCMD->m_DSMState < 3)
    {
        Logger::Log("zhx_GetDevicesList: ERROR - Failed to connect to DSM");
        if(deviceList && bufferSize > 0)
        {
            strncpy(deviceList, "{\"devices\":[],\"count\":0,\"error\":\"DSM not connected\"}", bufferSize - 1);
            deviceList[bufferSize - 1] = '\0';
        }
        return 0;
    }

    // 构建JSON响应
    std::ostringstream json;
    json << "{\"devices\":[";
    
    bool first = true;
    int count = 0;
    
    // 访问基类的m_DataSources（通过getDataSource方法）
    // 注意：我们需要通过TwainApp基类访问
    for(unsigned int i = 0; i < gpTwainApplicationCMD->m_DataSources.size(); ++i)
    {
        if(!first) json << ",";
        
        const TW_IDENTITY& ds = gpTwainApplicationCMD->m_DataSources[i];
        json << "{";
        json << "\"id\":" << ds.Id << ",";
        json << "\"manufacturer\":\"" << ds.Manufacturer << "\",";
        json << "\"productFamily\":\"" << ds.ProductFamily << "\",";
        json << "\"productName\":\"" << ds.ProductName << "\",";
        json << "\"version\":\"" << (int)ds.Version.MajorNum << "." << (int)ds.Version.MinorNum << "\"";
        json << "}";
        
        first = false;
        count++;
    }
    
    json << "],\"count\":" << count << "}";
    
    std::string jsonStr = json.str();
    if(deviceList && bufferSize > 0)
    {
        strncpy(deviceList, jsonStr.c_str(), bufferSize - 1);
        deviceList[bufferSize - 1] = '\0';
    }
    
    Logger::Log("zhx_GetDevicesList: Found %d devices", count);
    return 1;
}

//////////////////////////////////////////////////////////////////////////////
/**
 * 完整的扫描函数（包含消息循环，供HTTP服务器和外部调用）
 * 这个函数在C++中完成所有逻辑，包括消息循环，避免跨语言的线程问题
 * @param deviceId 设备ID（从1开始）
 * @param outputPath 输出路径（可以为NULL，使用默认路径）
 * @param timeoutMs 超时时间（毫秒），0表示使用默认值（30秒）
 * @return 1表示成功，0表示失败
 */
extern "C" __declspec(dllexport) int zhx_ScanComplete(int deviceId, const char* outputPath, int timeoutMs)
{
    if(!gpTwainApplicationCMD)
    {
        Logger::Log("zhx_ScanComplete: ERROR - gpTwainApplicationCMD is NULL! Call zhx_Init first.");
        return 0;
    }

    // 设置输出路径（如果提供）
    if(outputPath && strlen(outputPath) > 0)
    {
        gpTwainApplicationCMD->m_strSavePath = string(outputPath);
        Logger::Log("zhx_ScanComplete: Output path set to: %s", outputPath);
    }

    // 加载数据源
    Logger::Log("zhx_ScanComplete: Loading device with ID: %d", deviceId);
    gpTwainApplicationCMD->loadDS(deviceId);
    
    if(gpTwainApplicationCMD->m_DSMState < 4)
    {
        Logger::Log("zhx_ScanComplete: ERROR - Failed to load data source");
        return 0;
    }

    // 获取窗口句柄
    HWND hWnd = NULL;
    #ifdef TWH_CMP_MSC
    hWnd = GetConsoleWindow();
    if(!hWnd)
    {
        hWnd = GetDesktopWindow();
    }
    #endif

    if(!hWnd)
    {
        Logger::Log("zhx_ScanComplete: ERROR - Could not get valid window handle!");
        return 0;
    }

    // 启用DS
    Logger::Log("zhx_ScanComplete: Enabling data source...");
    gpTwainApplicationCMD->m_DSMessage = 0;
    
    if(!gpTwainApplicationCMD->enableDS(hWnd, FALSE))
    {
        Logger::Log("zhx_ScanComplete: ERROR - Failed to enable data source");
        return 0;
    }

    // 设置超时时间（默认30秒）
    if(timeoutMs <= 0)
    {
        timeoutMs = 30000;
    }

    // 运行消息循环（在C++中完成，避免跨语言的线程问题）
    Logger::Log("zhx_ScanComplete: Starting message loop...");
    DWORD startTime = GetTickCount();
    const DWORD timeout = (DWORD)timeoutMs;
    int loopCount = 0;
    const int maxLoops = 100000;  // 防止无限循环

    while(true)
    {
        loopCount++;
        
        // 超时检查
        if(GetTickCount() - startTime > timeout)
        {
            Logger::Log("zhx_ScanComplete: Message loop timeout after %d ms", timeout);
            gpTwainApplicationCMD->disableDS();
            return 0;
        }

        // 检查循环次数
        if(loopCount > maxLoops)
        {
            Logger::Log("zhx_ScanComplete: Message loop reached max loops: %d", maxLoops);
            gpTwainApplicationCMD->disableDS();
            return 0;
        }

        // 检查是否有DS消息
        if(gpTwainApplicationCMD->m_DSMessage != 0)
        {
            Logger::Log("zhx_ScanComplete: DS message received: %d", gpTwainApplicationCMD->m_DSMessage);
            
            if(gpTwainApplicationCMD->m_DSMessage == MSG_XFERREADY)
            {
                // 开始扫描
                Logger::Log("zhx_ScanComplete: Starting scan...");
                gpTwainApplicationCMD->m_DSMState = 6;
                gpTwainApplicationCMD->startScan();
                
                // 扫描完成后，禁用DS
                gpTwainApplicationCMD->disableDS();
                
                Logger::Log("zhx_ScanComplete: Scan completed successfully");
                return 1;
            }
            else if(gpTwainApplicationCMD->m_DSMessage == MSG_CLOSEDSREQ || 
                    gpTwainApplicationCMD->m_DSMessage == MSG_CLOSEDSOK)
            {
                Logger::Log("zhx_ScanComplete: DS closed by user or source");
                gpTwainApplicationCMD->disableDS();
                return 0;
            }
            else
            {
                Logger::Log("zhx_ScanComplete: Unknown DS message: %d", gpTwainApplicationCMD->m_DSMessage);
                gpTwainApplicationCMD->disableDS();
                return 0;
            }
        }

        // 处理Windows消息
        MSG msg;
        if(PeekMessage(&msg, NULL, 0, 0, PM_REMOVE))
        {
            if(msg.message == WM_QUIT)
            {
                Logger::Log("zhx_ScanComplete: Received WM_QUIT");
                gpTwainApplicationCMD->disableDS();
                return 0;
            }

            // 处理TWAIN事件
            if(!gUSE_CALLBACKS && 
               gpTwainApplicationCMD->getAppIdentity() && 
               gpTwainApplicationCMD->getDataSource())
            {
                TW_EVENT twEvent = {0};
                twEvent.pEvent = (TW_MEMREF)&msg;
                twEvent.TWMessage = MSG_NULL;
                
                TW_UINT16 twRC = _DSM_Entry(
                    gpTwainApplicationCMD->getAppIdentity(),
                    gpTwainApplicationCMD->getDataSource(),
                    DG_CONTROL,
                    DAT_EVENT,
                    MSG_PROCESSEVENT,
                    (TW_MEMREF)&twEvent);

                if(twRC == TWRC_DSEVENT)
                {
                    switch(twEvent.TWMessage)
                    {
                        case MSG_XFERREADY:
                        case MSG_CLOSEDSREQ:
                        case MSG_CLOSEDSOK:
                        case MSG_NULL:
                            gpTwainApplicationCMD->m_DSMessage = twEvent.TWMessage;
                            Logger::Log("zhx_ScanComplete: TWAIN event received: %d", twEvent.TWMessage);
                            continue;  // 继续循环，检查消息
                        default:
                            Logger::Log("zhx_ScanComplete: Unknown TWAIN message: %d", twEvent.TWMessage);
                            break;
                    }
                }
            }

            // 分发普通Windows消息
            TranslateMessage(&msg);
            DispatchMessage(&msg);
        }
        else
        {
            // 没有消息，短暂休眠
            Sleep(10);
        }
    }

    // 理论上不会到达这里
    Logger::Log("zhx_ScanComplete: Unexpected exit from message loop");
    gpTwainApplicationCMD->disableDS();
    return 0;
}
