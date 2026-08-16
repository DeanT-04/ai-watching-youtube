enum LotSizingEnum {
    LowRiskPreset = 5, //Low Risk Set 20% annual (0.25% load)
    MidRiskPreset = 4, //Mid Risk Set 40% annual (0.5% load)
    HighRiskPreset = 3, //Significant Risk Set 80% annual (1.0% load)
    ExtremeRiskPreset = 7, //High Risk Set 120% annual (1.5% load)
    LotsEquity = 2, //Dynamic Lot based on Equity
    LotsBalance = 1, //Dynamic Lot based on Balance
    LotsDepositLoad = 6, //Lots based on Deposit load
    FixedLots = 0 //Fixed Lot
};
enum AllowBuySellEnum {
    AllowSell2 = 2, //Sell only
    AllowBuy1 = 1, //Buy only
    AllowBuySell = 0 //Buy and Sell
};
enum eMaxDrawdownAction {
    IgnoreNewUntilRestart = 3, //Prohibit opening new grids until restart
    IgnoreNewSignals = 2, //Prohibit opening new grids
    CloseStopTradingUntilRestart = 1, //Close trades & stop trading until restart
    CloseStopTradingFor24h = 0 //Close trades & stop trading for 24h
};
enum eDrawdownCalculation {
    ThisStrategy = 1, //This strategy
    TheAccount = 0 //The account
};
