//+------------------------------------------------------------------+
//| Get the order type time as string                                |
//+------------------------------------------------------------------+
string CTrade::RequestTypeTimeDescription(void) const
{
    string str;
//--- l
    FormatOrderTypeTime(str,m_request.type_time);
    return(str);
}
//+------------------------------------------------------------------+
//| Get the result structure                                         |
//+------------------------------------------------------------------+
void CTrade::Result(MqlTradeResult &result) const
{
    result.retcode =m_result.retcode;
    result.deal =m_result.deal;
    result.order =m_result.order;
    result.volume =m_result.volume;
    result.price =m_result.price;
    result.bid =m_result.bid;
    result.ask =m_result.ask;
    result.comment =m_result.comment;
    result.request_id=m_result.request_id;
    result.retcode_external=m_result.retcode_external;
}
