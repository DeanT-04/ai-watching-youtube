CTrade::CTrade(void) : m_type_filling(ORDER_FILLING_FOK),
                       m_log_level(LOG_LEVEL_ERRORS)
{
    SetMarginMode();
//--- initialize protected data
    ClearStructures();
//--- check programm mode
    if(MQLInfoInteger(ENUM_MQL_INFO_INTEGER::MQL_TESTER))
        m_log_level=LOG_LEVEL_ALL;
    if(MQLInfoInteger(ENUM_MQL_INFO_INTEGER::MQL_OPTIMIZATION))
        m_log_level=LOG_LEVEL_ALL;
}
//+------------------------------------------------------------------+
//| Destructor                                                       |
//+------------------------------------------------------------------+
CTrade::~CTrade(void)
{
    m_log_level=LOG_LEVEL_NO;
}
//+------------------------------------------------------------------+
//| Get the request structure                                        |
//+------------------------------------------------------------------+
void CTrade::Request(MqlTradeRequest &request) const
{
    request.action =m_request.action;
    request.magic =m_request.magic;
    request.order =m_request.order;
    request.symbol =m_request.symbol;
    request.volume =m_request.volume;
    request.price =m_request.price;
