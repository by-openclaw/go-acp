/************************ RCV2 PC Test Application ***************************
*
*                               MAIN.C
*
* 	RCV2 PC Test Client Application - Main
*
*
*
*   Revision History
*   ----------------
*	20/02/2008		S Kellagher
*/

/*******************  Include files ***********************/

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#ifdef WIN32
#include <windows.h>

#else					// POSIX
#include <errno.h>
#include <arpa/inet.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/select.h>
#include <netinet/tcp.h>
#include <netdb.h>
#include <unistd.h>

#define _MAX_PATH                   256

#endif

#include <k_os/k_types.h>
#include <k_os/k_kernel.h>
#include <k_os/k_extra.h>

#ifdef WIN32
#include <k_os/win32/kosemu.h>
#else
#include <K_os/posix/kosemu.h>
#endif


#define BSP
#include <rollcall.h>
#include <rollcallv2/CoreServiceAPI.h>
#include <rollcallv2/core.h>
#include <rollcallv2/iam.h>
#include <rollcallv2/blockingclient.h>
#include <rollcallv2/controlclient.h>
#include <rollcallv2/blockingcontrolclient.h>
#include <rollcallv2/blockingmenuclient.h>
#include <rollcallv2/rcmessage.h>
#include <rollcallv2/ipshClient.h>



/*******************  Local macros  ************************/

/* Simple definitions for RollCall version  */
#define RC_MAJOR_VERSION            1       /* Major revision number */
#define RC_MINOR_VERSION            0       /* Minor revision number */
#define RC_ALPHA_VERSION            ' '     /* Alpha revision number */
#define RC_CMDSET_VERSION           1       /* Command set number */

#define UNIT_ADDRESS				0x84
#define CLIENT_NAME					"TestClient"

#define NUM_CORE_SESSIONS			300
#define NUM_PHYSICAL_IDS			2

#define RCMESSAGE_BUF_SIZE			4096


/**************** 5919 C O M M A N D   N U M B E R S   ***********************/

/* Well-known command numbers for audio input processing to suit RollCall Command set  */
#define CMD_VIDEO_SOURCE_INDEX			100
#define CMD_VIDEO_SOURCE_NAME			101

/* Delay enable and value in steps of 0.25 ms, range 0 - 2 secs */
#define CMD_DELAY_INPUT_N_BASE			200
#define CMD_DELAY_ENABLED_N_BASE		300

/* Gain (in steps of 0.1dB, with mute and phase invert checkboxes */
#define CMD_GAIN_INPUT_N_BASE			400
#define CMD_MUTE_INPUT_N_BASE			500
#define CMD_INVERT_INPUT_N_BASE			600

/* Shuffler control as read/write numeric, with single range instead of (Type, Offset) pair */
#define CMD_SHUFFLE_INPUT_N_BASE		700

/* Read-only values showing current shuffling as (Type, Offset) */
#define CMD_SHUFFLE_INDEX_N_BASE		732
#define CMD_SHUFFLE_TYPE_N_BASE			764

/* Shuffler memories and presets */
#define CMD_SHUFFLE_RECALL_MEM			799
#define CMD_SHUFFLE_MEM_SELECT			895
#define CMD_SHUFFLE_MEM_DO_SAVE			896
#define CMD_SHUFFLE_MEM_EDIT_NAME		897


/* User-friendly set/clear all controls */
#define CMD_DELAY_INPUT_ALL				299
#define CMD_DELAY_ENABLED_INPUT_ALL		399
#define CMD_GAIN_INPUT_ALL				499
#define CMD_MUTE_INPUT_ALL				599
#define CMD_INVERT_INPUT_ALL			699

#define NUM_5919_CHANNELS				16


/************************ Local Data *****************************************/

typedef struct
{
	VALUE_STR		Value;
	char			String [ LONGSTRLEN];

} VALUESTR_STRING;

typedef struct
{
	MENUITEM_STR	Func;
	char			String [2 * LONGSTRLEN];

} MENUITEMSTR_STRING;



/************   RollCall library pointers   ***********/

static P_RC_CORE_API						pRollCall;
static P_RC_IAM_API							pIAM;
static P_RC_BLOCKING_CLIENT_API				pClient;
static P_RC_CONTROL_CLIENT_API				pAsyncControlClient;
static P_RC_BLOCKING_CONTROL_CLIENT_API		pControlClient;
static P_RC_BLOCKING_MENU_CLIENT_API		pMenuClient;
static P_RC_MESSAGE_API						pMessageLogger;
static P_RC_IPSH_MULTI_CLIENT_API			pIPSHClient;



/************   Identities, etc  ***********/

static RC_IPSHCLIENT_CONN				Connection;

static FullAddress_t 					BcastAddress = {0, 0, 0, UNKNOWNSESS};

static RC_IAM_CONTROL					Iam_control;

static void                			   *IPIdentity;
static int								IPPhysical;
                        			
static int 								InStartup = 1;
static int								ExitFlag  = 0;

static UserIndex_t						MySession;

static 	FullAddress_t					MySelf = { 0, UNIT_ADDRESS, 0, UNKNOWNSESS };

static int								RequestsSent;
static int								RepliesReceived;


/*******************  Start of Code  ***********************/

/************   Callback functions  ***********/

void 	IpShConnectionStateCallback(RC_IPSHCLIENT_CONN *pConn, IPSHCLIENT_CONN_STATE State)
{
	printf("IpShConnectionStateCallback: State %d\n", State);

	// Test that application can do this...
	if (State != IPSHCLIENT_CONN_CONNECTED)
	{
		//pIPSHClient->DisConnect(pConn);
	}

}


void	ConnectCallback (int Result, UserIndex_t SessionID, KOS_BUF_ID Message, KOS_BUF_ID Reply, void *Context)
{
	printf("\nConnectCallback: result = %d, session = %u\n", Result, SessionID);
}

void	TermCallback 	(int Result, UserIndex_t SessionID, KOS_BUF_ID Message, KOS_BUF_ID Reply, void *Context)
{
	printf("\nTermCallback: result = %d, session = %u\n", Result, SessionID);
}

void	BCSessionHangup (int Result, UserIndex_t SessionID, KOS_BUF_ID Message, KOS_BUF_ID Reply, void *Context)
{
	printf("\nBCSessionHangup: result = %d, session = %u\n", Result, SessionID);
}

PacketType_t	BCPacketHandler(PacketType_t PacketType, UserIndex_t SessionID, KOS_BUF_ID Message, void *Context)
{
	//printf("\nBCPacketHandler: got %s, session = %u\n", pRollCall->GetPacketNameByType(PacketType), SessionID);

	if (Message != NEAR_NULL)
	{
		MSG_QUE_ENTRY  *MsgData = RC_BUF_DATA(Message);  

		pRollCall->SwopToProcessorOrder (MsgData, ORDER_ALL);


		switch(PacketType)
		{
			case SP_RETVALUE:
				{
					VALUE_STR *pValue = (VALUE_STR *)MsgData->Contents.Data;

					printf("RetValue: command = %lu, mode = %04x, Val = %ld\n", pValue->rCommand, pValue->rMode, pValue->rValue);
					if (pValue->rMode & FS_STRING)
					{
						printf ("String = %s\n", (char *)AFTER(pValue));
					}
				}
				break;

				
			case SP_RETMENUITEM:
				{
					MENUITEM_STR *pMenu = (MENUITEM_STR *)MsgData->Contents.Data;
					char *pText = (char *)AFTER(pMenu);

					printf("MENUITEM_STR:");
					printf(" rMenuIndex: %lu,",	pMenu->rMenuIndex);
					printf(" rStyle: %u,", 		pMenu->rStyle);
					printf(" rCommand: %lu,", 	pMenu->rCommand);
					printf(" rMinRange: %ld,", 	pMenu->rMinRange);
					printf(" rMaxRange: %ld,", 	pMenu->rMaxRange);
					printf(" rStep: %u,", 		pMenu->rStep);
					printf(" rDivScale: %u,", 	pMenu->rDivScale);

					printf(" szText: %s,", 			pText);
					printf(" szParamString: %s\n",	pText + strlen(pText) + 1);

				}
				break;

			default:
				break;
		}
	}
	return (SP_ACK);
}

static CLIENT_CALLBACK_TAB	MyClientCallback = 
{
	ConnectCallback, 
	TermCallback,
	BCSessionHangup,
	BCPacketHandler
};



/*******    CLI options   ***********/

int CLI_Exit(int argc, char* argv[])
{
	printf("Exiting...\n");
	ExitFlag = 1;
	return 0;
}



int CLI_IPConnect(int argc, char* argv[])
{
	int Result;
	
	printf("Connecting IPShClient connection....");
	Result = pIPSHClient->Connect(&Connection);
	pRollCall->SetDefaultInterface(Connection.PhysicalID);
	printf("Result = %d\n", Result);
	return(0);
}	


int CLI_IPDisconnect(int argc, char* argv[])
{
	int Result;
	
	printf("Disconnecting IPShClient connection....");
	Result = pIPSHClient->DisConnect(&Connection);
	printf("Result = %d\n", Result);
	return(0);
}	


int CLI_Drop(int argc, char* argv[])
{
	printf("Dropping IPShClient connection....");
	pIPSHClient->DisConnect(&Connection);
	return(0);
}	


int CLI_Connect(int argc, char* argv[])
{
	int Net, Unit, Port;
	FullAddress_t	Target;
	int	Result;
	Service_t Services = SV_MENUS | SV_CONTROL | SV_LONGSTR;

	sscanf(argv[1],"%04x:%02x:%02x",&Net, &Unit, &Port);
	Target.rNet = Net;
	Target.rUnit = Unit;
	Target.rPort = Port;
	Target.rIndex = UNKNOWNSESS;

	printf("Connecting to %04x:%02x:%02x ...\n", Target.rNet, Target.rUnit, Target.rPort);
	Result = pClient->Connect(Target, MySelf, 0, Services, &MySession, UL_SUPERVISOR, &MyClientCallback, NULL, "MySession");
	printf("Result = %d, on session %u\n", Result, MySession);
	return 0;
}

int CLI_Term(int argc, char* argv[])
{
	int Result;
	
	printf("Sending term...");
	Result = pClient->Term (MySession, 0, "User");
	printf("Result = %d\n", Result);
	return(0);
}	

int CLI_BCReady(int argc, char* argv[])
{
	int State = (argc > 1) ? atoi(argv[1]) : 1;
	int Result;
	
	printf("Setting Backchannel to state %d...", State);
	Result = pClient->SetBCReady (MySession, (UBYTE)State);
	printf("Result = %d\n", Result);
	return(0);
}	

int CLI_ReportChanges(int argc, char* argv[])
{
	int State = (argc > 1) ? atoi(argv[1]) : 1;
	int Result;
	
	printf("Setting ReportChanges to state %d...", State);
	Result = pControlClient->ReportChanges (MySession, 0xFFFF, State);
	printf("Result = %d\n", Result);
	return(0);
}	


int CLI_GetParam(int argc, char* argv[])
{
	Command_t Command = atoi(argv[1]);
	int Result;
	VALUESTR_STRING	RetValue;

	printf("GetValue for command %u...",  Command);
	Result = pControlClient->SendGetValue(MySession, (ExtCommand_t)Command, &RetValue.Value);
	printf("Result = %d\n", Result);

	if (Result == RC_REPLY_RX)
	{
		printf("RetValue: command = %lu, mode = %04x, Val = %ld\n", RetValue.Value.rCommand, RetValue.Value.rMode, RetValue.Value.rValue);
		if (RetValue.Value.rMode & FS_STRING)
		{
			printf ("String = %s\n", RetValue.String);
		}
	}
	return(0);
}


int CLI_SetParam(int argc, char* argv[])
{
	int Result;
	VALUESTR_STRING	SetValue;
	VALUESTR_STRING	RetValue;

	SetValue.Value.rCommand = atol(argv[1]);
	SetValue.Value.rMode    = atoi(argv[2]);

	if (SetValue.Value.rMode & FS_STRING)
	{
		strsafecpy(SetValue.String, argv[3], MAXTEXTSIZE-1);
		SetValue.Value.rValue = 0L;
	}
	else
	{
		SetValue.String[0] = '\0';
		SetValue.Value.rValue = atol(argv[3]);
	}

	printf("SetValue for command %lu, mode %u, numval %ld, string %s...", 
				SetValue.Value.rCommand, SetValue.Value.rMode, SetValue.Value.rValue, SetValue.String);

	Result = pControlClient->SendSetValue(MySession, &SetValue.Value, &RetValue.Value);

	printf("Result = %d\n", Result);

	if (Result == RC_REPLY_RX)
	{
		printf("RetVal: command = %lu, mode = %04x, Val = %ld\n", RetValue.Value.rCommand, RetValue.Value.rMode, RetValue.Value.rValue);
		if (RetValue.Value.rMode & FS_STRING)
		{
			printf ("String = %s\n", RetValue.String);
		}
	}

	return(0);
}


int CLI_RequestMenuSet(int argc, char* argv[])
{
	int Result;
	ExtMenuIndex_t MenuBase = atol(argv[1]);
	ExtCount_t NumLines = 0;

	printf("GetMenuCount for menubase %u...", MenuBase);
	Result = pMenuClient->GetMenuCount(MySession, MenuBase, &NumLines);
	printf("Result = %d, NumLines = %lu\n", Result, NumLines);
	return(0);
}


int CLI_LoadMenuLine(int argc, char* argv[])
{
	int Result;

	ExtMenuIndex_t MenuIndex = atol(argv[1]);
	MENUITEMSTR_STRING RetMenu;

	printf("GetMenuItem for menu index %lu...", MenuIndex);
	Result = pMenuClient->GetMenuItem(MySession, MenuIndex, &RetMenu.Func);
	printf("Result = %d\n", Result);

	if (Result == RC_REPLY_RX)
	{
		printf("MENUITEM_STR:");
		printf(" rMenuIndex: %lu,",	RetMenu.Func.rMenuIndex);
		printf(" rStyle: %u,", 		RetMenu.Func.rStyle);
		printf(" rCommand: %lu,", 	RetMenu.Func.rCommand);
		printf(" rMinRange: %ld,", 	RetMenu.Func.rMinRange);
		printf(" rMaxRange: %ld,", 	RetMenu.Func.rMaxRange);
		printf(" rStep: %u,", 		RetMenu.Func.rStep);
		printf(" rDivScale: %u,", 	RetMenu.Func.rDivScale);

		printf(" szText: %s,", 			RetMenu.String);
		printf(" szParamString: %s\n",	RetMenu.String + strlen(RetMenu.String) + 1);
	}	

	return(0);
}


/*
 * Callback function for asynchronous read function below.
 * For now, just check command number and count replies received.
 * Command requested is passed in as Context for convenience.
 */
static void GetValueCallback (int Result, UserIndex_t SessionID, KOS_BUF_ID Message, KOS_BUF_ID Reply, void *Context)
{
	// Should always have a valid outgoing message buf, and valid context
	if ((Message != NEAR_NULL) && (Context != NULL))
	{
		MSG_QUE_ENTRY  *pSentData = RC_BUF_DATA(Message);
		ExtCommand_t Requested = (ExtCommand_t)Context;
		
		// Now, check the result and Reply buffer
		if ((Result == RC_REPLY_RX) && (Reply != NEAR_NULL))
		{
			// Now check the Reply
			MSG_QUE_ENTRY  *ReplyData = RC_BUF_DATA(Reply);  

			// Get into processor order
			pRollCall->SwopToProcessorOrder (ReplyData, ORDER_ALL);
			
			if (ReplyData->Contents.sRollHeader.rPktType == SP_RETVALUE)
			{
			    VALUE_STR *pValue = (VALUE_STR *)ReplyData->Contents.Data;
				
				if (pValue->rCommand == Requested)
				{
					++RepliesReceived;
				}
			}
		}
	}
}

/*
 * Function to read current audio node settings using asynchronous/non-blocking control client.
 */
int CLI_AsyncReadAudioNode(int argc, char* argv[])
{
	int	Result;
	Count_t idx;
	clock_t start_time, end_time;

	/* Enable multiple messages in flight on this session */
//	pRollCall->EnableMultiMessageOperation(MySession);

	start_time = clock();
	Result = 0;
	RequestsSent = 0;
	RepliesReceived = 0;
	printf("Fetching current values...\n");
	
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_DELAY_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_DELAY_ENABLED_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_GAIN_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_MUTE_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_INVERT_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_SHUFFLE_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}

	printf("Waiting for replies...\n");
	for (idx = 1; idx < 500; idx++)
	{
		if (RepliesReceived == RequestsSent)
		{
			break;
		}
		TaskDelay(10);
	}
	end_time = clock();
	printf("Result = %d, sent %d, received %d took %lu clocks\n", Result, RequestsSent, RepliesReceived, end_time - start_time);
	return 0;
}


/*
 * Function to read current audio node settings using synchronous/blocking control client.
 */
int CLI_SyncReadAudioNode(int argc, char* argv[])
{
	int	Result;
	Count_t idx;
	clock_t start_time, end_time;
	VALUESTR_STRING	RetValue;

	start_time = clock();
	Result = 0;
	printf("Fetching current values...\n");
	
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_DELAY_INPUT_N_BASE + idx;
		Result |= pControlClient->SendGetValue(MySession, Cmd,  &RetValue.Value);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_DELAY_ENABLED_N_BASE + idx;
		Result |= pControlClient->SendGetValue(MySession, Cmd,  &RetValue.Value);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_GAIN_INPUT_N_BASE + idx;
		Result |= pControlClient->SendGetValue(MySession, Cmd,  &RetValue.Value);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_MUTE_INPUT_N_BASE + idx;
		Result |= pControlClient->SendGetValue(MySession, Cmd,  &RetValue.Value);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_INVERT_INPUT_N_BASE + idx;
		Result |= pControlClient->SendGetValue(MySession, Cmd,  &RetValue.Value);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_SHUFFLE_INPUT_N_BASE + idx;
		Result |= pControlClient->SendGetValue(MySession, Cmd,  &RetValue.Value);
		++RequestsSent;
	}

	end_time = clock();
	printf("Result = %d, took %lu clocks\n", Result, end_time - start_time);
	return 0;
}


/*
 * This function illustrates the full sequence for connecting to an audio node,
 * enabling the back channel and control function reporting, and retrieving all
 * values using the asynchronous / non-blocking client. Then terminating the session.
 */
int CLI_AudioNodeConnectSequence(int argc, char* argv[])
{
	int Net, Unit, Port;
	FullAddress_t	Target;
	int	Result;
	int State = 1;
	MenuIndex_t MenuBase = 0;
	Count_t	NumLines = 0;
	Count_t idx;
	clock_t start_time, end_time;
	int 		RequestsSent;

	sscanf(argv[1],"%04x:%02x:%02x",&Net, &Unit, &Port);
	Target.rNet = Net;
	Target.rUnit = Unit;
	Target.rPort = Port;
	Target.rIndex = UNKNOWNSESS;

	printf("Connecting to %04x:%02x:%02x ...\n", Target.rNet, Target.rUnit, Target.rPort);
	Result = pClient->Connect(Target, MySelf, 0, SV_MENUS | SV_CONTROL | SV_LONGSTR, &MySession, UL_SUPERVISOR, &MyClientCallback, NULL, "MySession");
	printf("Result = %d, on session %u\n", Result, MySession);
	if (Result != 0)
	{
		return 0;
	}

	printf("Setting Backchannel to state %d...", State);
	Result = pClient->SetBCReady (MySession, (UBYTE)State);
	printf("Result = %d\n", Result);
	if (Result != 0)
	{
		return 0;
	}
	
	printf("Setting ReportChanges to state %d...", State);
	Result = pControlClient->ReportChanges (MySession, 0xFFFF, State);
	printf("Result = %d\n", Result);
	if (Result != 0)
	{
		return 0;
	}
	printf("Waiting for back channel menu updates to pass\n");
	TaskDelay(1000);

	/* Enable multiple messages in flight on this session */
//	pRollCall->EnableMultiMessageOperation(MySession);

	start_time = clock();
	printf("Fetching current values...\n");
	RequestsSent = RepliesReceived = 0;

	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_DELAY_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_DELAY_ENABLED_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_GAIN_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_MUTE_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_INVERT_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}
	for (idx = 1; idx <= NUM_5919_CHANNELS; idx++)
	{
		ExtCommand_t Cmd = CMD_SHUFFLE_INPUT_N_BASE + idx;
		Result |= pAsyncControlClient->SendGetValue(MySession, Cmd, GetValueCallback, (void *)Cmd);
		++RequestsSent;
	}

	printf("Waiting for replies...\n");

	for (idx = 1; idx < 1000; idx++)
	{
		if (RepliesReceived == RequestsSent)
		{
			break;
		}
		TaskDelay(10);
	}
	end_time = clock();
	printf("Result = %d, sent %d, received %d took %lu clocks\n", Result, RequestsSent, RepliesReceived, end_time - start_time);

	printf("Disconnecting from %04x:%02x:%02x ...", Target.rNet, Target.rUnit, Target.rPort);
	Result = pClient->Term(MySession, TC_USER, "Test");
	printf("Result = %d, on session %u\n", Result, MySession);
	if (Result != 0)
	{
		return 0;
	}


	Result = pClient->Term (MySession, 0, "User");
	return 0;
}



static COMMAND_SET  		MainCommands[] =
{
	{	1,	"exit"    ,		CLI_Exit	},
	{	1,	"ipcon" ,		CLI_IPConnect	},
	{	1,	"ipdis" ,		CLI_IPDisconnect	},
	{	1,	"drop" ,		CLI_Drop	},

	{	2,	"audionode" ,	CLI_AudioNodeConnectSequence	},

	{	2,	"connect" ,		CLI_Connect	},
	{	1,	"term" ,		CLI_Term	},
	{	2,	"bcready" ,		CLI_BCReady	},
	
	{	2,	"repchanges" ,	CLI_ReportChanges	},
	{	2,	"getparam" ,	CLI_GetParam	},
	{	4,	"setparam" ,	CLI_SetParam	},

	{	2,	"reqmenu" ,		CLI_RequestMenuSet	},
	{	2,	"loadmenu" ,	CLI_LoadMenuLine	},

	{	1,	"asyncread" ,	CLI_AsyncReadAudioNode	},
	{	1,	"syncread" ,	CLI_SyncReadAudioNode	},

	{	0,	"" 		  ,		NULL			}
};




static void StartRollCall (char *IPAddressStr, int IPClientPort)
{
	int	 ErrCode;
	
	// Register all the libraries needed by any variant
    (void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_CoreApi);
    (void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_IamApi);
	(void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_BlockingClientApi);
	(void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_ControlClientApi);
	(void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_BlockingControlClientApi);
	(void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_BlockingMenuClientApi);
    (void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_RcMessageApi);
    (void)RegisterLibrary ((P_LIB_JUMP_TAB)&RCL_IPSHMultiClientApi);


    /* initialise all the RollCall library pointers */
    pRollCall           = (P_RC_CORE_API)					 GetLibrary(RCL_CORE_NAME);
    pIAM                = (P_RC_IAM_API)    				 GetLibrary(RCL_IAM_NAME);
    pClient         	= (P_RC_BLOCKING_CLIENT_API)    	 GetLibrary(RCL_BLOCKING_CLIENT_NAME);
    pAsyncControlClient = (P_RC_CONTROL_CLIENT_API)			 GetLibrary(RCL_CONTROL_CLIENT_NAME);
    pControlClient      = (P_RC_BLOCKING_CONTROL_CLIENT_API) GetLibrary(RCL_BLOCKING_CONTROL_CLIENT_NAME);
    pMenuClient         = (P_RC_BLOCKING_MENU_CLIENT_API)    GetLibrary(RCL_BLOCKING_MENU_CLIENT_NAME);
	pMessageLogger		= (P_RC_MESSAGE_API)    			 GetLibrary(RCL_RCMESSAGE_NAME);
	pIPSHClient			= (P_RC_IPSH_MULTI_CLIENT_API)		 GetLibrary(IPSH_MULTI_CLIENT_NAME);

    (void)pRollCall->CoreInit ( NUM_CORE_SESSIONS, 0, 0);

    IPIdentity = pRollCall->CreateIdentity (	UNIT_ADDRESS, 
												0, 
												ID_RC32_ROUTING_IPSH_CLIENT, 
												RC_MAJOR_VERSION,
												RC_MINOR_VERSION,
												RC_ALPHA_VERSION,
												RC_CMDSET_VERSION,
												"TestClient",
												"TestClient"	);

    if (IPIdentity == NULL)
    {
        (void)fprintf(stderr, "CreateIdentity failed\n");
        exit(-1);
    }

	ErrCode = pIPSHClient->Init	(	RCL_CORE_NAME, 
									40,								// ThreadPriority
									10,								// Max connections
		                            DELAY_1S,		               	// Probe interval
									2,								// LifeCount
									1,								// Verbose
		                            IpShConnectionStateCallback  	// Callback for lib on change of connection state
                            );
   

	(void)pClient->ClientInit(RCL_CORE_NAME);
	(void)pAsyncControlClient->ControlClientInit(RCL_CORE_NAME);
	(void)pControlClient->ControlClientInit(RCL_CORE_NAME);
	(void)pMenuClient->MenuClientInit(RCL_CORE_NAME);

    (void)pMessageLogger->MessageInit(RCL_CORE_NAME, RCMESSAGE_BUF_SIZE);


	Connection.Name = "MyConn";
	Connection.RemoteIpTarget = IPAddressStr;
	Connection.RemoteTcpPort  = IPClientPort;
	Connection.Identity   = IPIdentity;

	pIPSHClient->Connect(&Connection);
	pRollCall->SetDefaultInterface(Connection.PhysicalID);

    (void)pIAM->IamInit(RCL_CORE_NAME, &Iam_control, 0);
}

static void StopRollCall(void)
{
    pMessageLogger->MessageClean();
	pMenuClient->MenuClientClean();
	pControlClient->ControlClientClean();
	pClient->ClientClean();
    pIAM->IamClean(&Iam_control);
    pIPSHClient->Clean();
    pRollCall->CoreClean();
}



// The main loop
int main  (int argc, char *argv[])
{
	char  *IpAddressStr;
	int   IpClientPort;
#ifdef WIN32
	WSADATA	wsadata;
#endif

	
    // Send welcome banner
    kprintf("\r\nRollCall Test Client\r\n-----------------");

	IpClientPort = 27;
	printf("Client port = %f%", (float)IpClientPort);


	if (argc < 3)
	{
		kprintf("Please specify IP_address and Port\n");
		return (0);
	}
	
	IpAddressStr = argv[1];
	IpClientPort = atoi(argv[2]);
	kprintf("Connecting to %s port %d\n", IpAddressStr, IpClientPort);

	InitialiseKernel();

    (void)RegisterCommandSet(MainCommands, "");

#ifdef WIN32
	if (WSAStartup(MAKEWORD(1,1), &wsadata) != 0)
	{
		printf("ERROR: couldn't start up WinSock2 DLL!\n");
		return -1;
	}
#endif

    StartRollCall(IpAddressStr, IpClientPort);

	CLI_Init();

    InStartup = 0;
   	kprintf("Client$");

	while (ExitFlag == 0)
    {
    	char InputBuffer [256];
    	KOS_BUF_ID	CmdBuf;
    	
    	fgets(InputBuffer, 256, stdin);
    	
    	CmdBuf = BufCreate();
    	if (CmdBuf)
    	{
	    	strcpy((char*)BufData(CmdBuf), InputBuffer);
	    	QueIn(GetCmdQue(), CmdBuf, KOS_WAIT_FOREVER);
	    }
	    
	    // To allow the CLI task to do its thing and update ExitFlag before we hit fgets() again
	    Sleep(50);
    }

	StopRollCall();
	CleanKernel();
    return(0);
}





