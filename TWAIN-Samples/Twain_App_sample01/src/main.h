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
* @file main.h
* main header file the common includes and defines
* @author TWAIN Working Group
* @date October 2007
*/

#ifndef __MAIN_H__
#define __MAIN_H__

#include "Common.h"
#include "CommonTWAIN.h"

// DLL 导出宏定义
#ifdef ZHX_TWAIN_EXPORTS
#define ZHX_TWAIN_API __declspec(dllexport)
#else
#define ZHX_TWAIN_API __declspec(dllimport)
#endif

#ifdef TWH_CMP_MSC
  #include <tchar.h>

  // TODO: reference additional headers your program requires here
  #include <io.h>
  #include <iostream>

  void printWindowsErrorMessage(); // defined in Twain_DS_sample01.cpp
#else
  #include <stdlib.h>
  #include <iostream>

#endif // TWH_CMP_MSC


TW_UINT16 CALLBACK ImageCallback(pTW_IDENTITY pOrigin, 
                                 pTW_IDENTITY pDest, 
                                 TW_UINT32 DG,
                                 TW_UINT16 DAT,
                                 TW_UINT16 MSG,
                                 TW_MEMREF pData);
// 导出函数声明
typedef int (*ScanCallback)(char *filename);
const char* getCapabilityChineseLabel(TW_UINT16 capValue);
void checkSupportedFormats();
void initBasicCapabilityMap();
void updateCapabilityMapFromDevice(const char* device);
/**
 * @brief 根据扫描仪名称获取对应的设备编号
 * 
 * 此函数从可用扫描仪列表中查找指定名称的扫描仪，并返回其设备编号。
 * 设备编号是用于loadDS()方法的参数，从1开始计数。
 * 
 * @param device 扫描仪名称
 * @return 成功返回设备编号(>0)，失败返回0
 */
static int zhx_GetDeviceNumber(const char *device);
/**
 * 转换TWAIN项目类型为字符串
 * @param itemType TWAIN数据类型ID
 * @return 对应的类型名称字符串
 */
static const char* convertItemTypeToString(TW_UINT16 itemType);

/**
 * 获取能力值的描述标签
 * @param capValue 能力ID
 * @param itemValue 能力值
 * @return 对应值的描述标签
 */
static const char* getCapabilityValueLabel(TW_UINT16 capValue, int itemValue);

extern "C" __declspec(dllexport) void  zhx_twain_test();

extern "C" __declspec(dllexport) void  zhx_twain();

extern "C" __declspec(dllexport) void  zhx_Init();
// int zhx_ApproveLicenseA(char *license);
extern "C"  __declspec(dllexport)  char*  zhx_GetDevicesList();
//extern "C" char* __declspec(dllexport)  zhx_GetDevCapability_JSON(char *device);
//extern "C" int __declspec(dllexport)  zhx_SetCapability_STR(char *nCap, char *value);
extern "C" __declspec(dllexport) int  zhx_OpenDevice(char *device);
extern "C" __declspec(dllexport) int  zhx_Scan(char *path, ScanCallback cb, int count);
extern "C" __declspec(dllexport) void  zhx_EndScan();
extern "C" __declspec(dllexport) void  zhx_CloseDevice();
extern "C" __declspec(dllexport) void  zhx_Exit();
// 状态查询：让调用方不必在外面自己记一份可能过期的连接状态
extern "C" __declspec(dllexport) int   zhx_GetState();
extern "C" __declspec(dllexport) const char* zhx_GetCurrentDevice();
extern "C" __declspec(dllexport) int zhx_SetTransferMechanism(int mechanism);
extern "C" __declspec(dllexport) int zhx_SetImageFileFormat(int format);
extern "C" __declspec(dllexport) int zhx_GetCurrentFileFormat();
extern "C" __declspec(dllexport) char* zhx_GetSupportedFileFormats();
extern "C" __declspec(dllexport) int zhx_SetResolution(int dpi);
extern "C" __declspec(dllexport) char* zhx_GetSupportedResolutions();
extern "C" __declspec(dllexport) int zhx_GetCurrentResolution();
extern "C" __declspec(dllexport) char* zhx_GetDevCapability_JSON(char *device);
extern "C" __declspec(dllexport) char* zhx_GetCapability_STR(char* capOrDevice);
extern "C" __declspec(dllexport) int zhx_SetCapability_STR(char *nCap, char *value);
extern "C" __declspec(dllexport) char* zhx_GetDevCapability_STR(char* device);
#endif //__MAIN_H__
