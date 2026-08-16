//+------------------------------------------------------------------+
//|                                                  WakaWaka by MrCapFree.mq5 |
//|                                                                  Mr CapFree |
//|                                              https://www.MrCapFree.com |
//+------------------------------------------------------------------+
#property copyright "Mr CapFree"
#property link   "https://www.MrCapFree.com"
#property version "1.00"
#property strict
#property description "Waka Waka EA"
//+------------------------------------------------------------------+
//| Include                                                          |
//+------------------------------------------------------------------+
#include <Trade\Trade.mqh>
CTrade Trade;
CPositionInfo posInfo;
COrderInfo ordinfo;
//+------------------------------------------------------------------+
//| Expert initialization function                                   |
//+------------------------------------------------------------------+
int OnInit() {
    return(INIT_SUCCEEDED);
}
//+------------------------------------------------------------------+
//| Expert deinitialization function                                 |
//+------------------------------------------------------------------+
void OnDeinit(const int reason) {
}
