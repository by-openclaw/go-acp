Thank you for your interest in the Snell & Wilcox SNMP MIBs.
The following products are supported by these MIBs:

1. IQ Modular 3U Chassis fitted with an Ethernet/SNMP Gateway card
   version 4.0 or greater
2. RollMap Network Management System Version 1.7 or greater
3. RollCall Middleware Services version 3.5 or greater
4. IQ Modular 3U Chassis fitted with an Ethernet Gateway card
   version 2.1 or greater - legacy "chassis monitoring only" MIBs.

Please Unzip all files to the correct folder for your MIB Compile to use.

Please note: the Snell & Wilcox product range uses structured MIBs.
If the MIB compiler for the SNMP manager you are using does not support
dynamic binding of MIB files at compile time, you will need to manually
compile the files one at a time in the order listed below.

All published Snell & Wilcox MIBs can be downloaded in the zip file
All_SandW_MIBs.zip


-----------------------------------------------------------------------
1. IQ 3U Chassis with Ethernet/SNMP Gateway card version 4.0 or greater
-----------------------------------------------------------------------

The current Snell & Wilcox Modular 3U system supports full SNMP control and
monitoring of all parameters of all RollCall-enabled modules, and the chassis
itself.  Any parameter that can be accessed through RollCall control and/or
RollCall logging is now fully accessible via SNMP from any SNMP Manager.
All IQH3U-S systems shipped since October 2005 now include this advanced
SNMP-enabled gateway as standard.

MIBs required for IQ chassis (V4.x or later) are:
SNELL-WILCOX-SMI.mib
SNELL-WILCOX-TC.mib
SNELL-WILCOX-PRODUCT-REG.mib
SNELL-WILCOX-UNIT.mib
SNELL-WILCOX-GATEWAY-LOGGING.mib
SNELL-IQH3A-CMD-MIB.mib
PLUS the MIB files for the individual IQ modules, SNELL-IQxxx-CMD-MIB.mib.
For example, if the chassis contains IQSYN21 and IQUAV01 modules, then you need the MIBs:
SNELL-IQSYN21-CMD-MIB.mib 
SNELL-IQUAV01-CMD-MIB.mib 

The zip file All_IQ_Modular_MIBs.zip contains the complete set of IQ modular MIBs.


Note:  All IQ Mibs were changed on 18th July '06, to correct minor syntax errors
that caused non-fatal warning messages from some MIB compilers.  Therefore
the majority of integer enum value descriptions have changed name, e.g.
NoAction(0) changed to noAction(0).


-----------------------------------------------------------
2. RollMap Network Management System Version 1.7 or greater
3. RollCall Middleware Services version 3.5 or greater
-----------------------------------------------------------

These two software packages allow alarms from any RollCall devices to be converted to SNMP traps.
Any logging parameter that can be viewed in the RollMap or LogViewer GUIs can be sent as traps
to any SNMP Manager application.

MIBs required for RollMap NMS or RollCall Middleware Services are:
SNELL-WILCOX-SMI.mib
SNELL-WILCOX-TC.mib
SNELL-WILCOX-PRODUCT-REG.mib
SNELL-WILCOX-UNIT.mib
SNELL-WILCOX-ROLLMAP

The zip file All_RollMap_MIBs.zip contains all the RollMap and Middleware Services MIBs.


------------------------------------------------------------------
4. IQ 3U Chassis with Ethernet Gateway card version 2.1 or greater
   (legacy "chassis monitoring only" MIBs)
------------------------------------------------------------------

The previous generation of Snell & Wilcox Modular 3U system
supported basic SNMP monitoring of the chassis, such as
power supply, fan, temperature, and list of installed modules.

For IQH3U-S systems shipped since October 2005, we recommend the full
MIBs - see item 1 above.  However the legacy MIBs are still supported,
keeping full backward compatibility with monitoring systems designed
for the V2 or V3 IQH3U gateways.

MIBs required for IQ chassis (V2.x or later) are:
SNELL-WILCOX-SMI.mib
SNELL-WILCOX-TC.mib
SNELL-WILCOX-PRODUCT-REG.mib
SNELL-WILCOX-UNIT.mib
SNELL-WILCOX-MODULAR-GATEWAY.mib

The zip file All_Legacy_IQ_Modular_MIBs.zip contains the MIBs for V2.x or later IQ 3U systems.


------------------------------------------------------------------


Any support queries regarding the MIB files and SNMP operation should be forwarded by email to
RollCall@SnellWilcox.com
Or contact your local support representative.

Many Thanks,

The RollCall Team.

Last Updated 18th July 2006
CVS $Id: PublishedReadMe.txt,v 1.3 2006/07/18 14:57:34 mauricesnell Exp $
