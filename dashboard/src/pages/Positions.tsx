import { Fragment, useEffect, useState } from 'react';
import { getPositions, getTrades } from '../api/client';

interface Position {
  id: string;
  token_id: string;
  outcome: string;
  size: string;
  avg_price: string;
  realized_pnl: string;
  winner: boolean | null;
  question: string;
  is_open: boolean;
  created_at: string;
  net_size: string;
}

interface Trade {
  id: string;
  asset_id: string;
  side: string;
  price: string;
  size: string;
  status: string;
  match_time: string;
  outcome: string;
  winner: boolean | null;
  profit_loss: number | null;
}

const LIMIT = 20;

function ResultBadge({ winner }: { winner: boolean | null }) {
  if (winner === null) return <span className="bg-gray-100 text-gray-500 text-xs px-2 py-0.5 rounded-full">Settled</span>;
  return winner
    ? <span className="bg-green-100 text-green-700 text-xs px-2 py-0.5 rounded-full font-medium">Won</span>
    : <span className="bg-red-100 text-red-700 text-xs px-2 py-0.5 rounded-full font-medium">Lost</span>;
}

function TradeResultBadge({ side, winner }: { side: string; winner: boolean | null }) {
  if (side !== 'BUY') return <span className="text-gray-400 text-xs">—</span>;
  if (winner === null) return <span className="bg-gray-100 text-gray-500 text-xs px-2 py-0.5 rounded-full">Pending</span>;
  return winner
    ? <span className="bg-green-100 text-green-700 text-xs px-2 py-0.5 rounded-full font-medium">Win</span>
    : <span className="bg-red-100 text-red-700 text-xs px-2 py-0.5 rounded-full font-medium">Loss</span>;
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

export default function Positions() {
  const [activeTab, setActiveTab] = useState<'open' | 'closed'>('open');
  const [positions, setPositions] = useState<Position[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [tradesByToken, setTradesByToken] = useState<Map<string, Trade[]>>(new Map());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Load trades once for the expand sub-table
  useEffect(() => {
    getTrades({ limit: 1000 })
      .then(res => {
        const map = new Map<string, Trade[]>();
        for (const t of (res.data.trades || []) as Trade[]) {
          const list = map.get(t.asset_id) || [];
          list.push(t);
          map.set(t.asset_id, list);
        }
        setTradesByToken(map);
      })
      .catch(() => {});
  }, []);

  // Load positions when tab or page changes
  useEffect(() => {
    setLoading(true);
    setExpanded(new Set());
    getPositions({ tab: activeTab, limit: LIMIT, offset: (page - 1) * LIMIT })
      .then(res => {
        setPositions(res.data.positions || []);
        setTotal(res.data.total || 0);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [activeTab, page]);

  const switchTab = (tab: 'open' | 'closed') => {
    setActiveTab(tab);
    setPage(1);
  };

  const toggleExpand = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  const totalPages = Math.ceil(total / LIMIT);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Positions</h1>

      <div className="bg-white rounded-lg shadow overflow-hidden">
        {/* Tab bar */}
        <div className="flex border-b border-gray-200 px-0">
          <button
            onClick={() => switchTab('open')}
            className={`px-6 py-3 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'open'
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            Open
          </button>
          <button
            onClick={() => switchTab('closed')}
            className={`px-6 py-3 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'closed'
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            Closed
          </button>
        </div>

        {loading ? (
          <div className="text-center py-8 text-gray-500">Loading...</div>
        ) : positions.length === 0 ? (
          <div className="p-8 text-center text-gray-500">
            No {activeTab} positions.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                {activeTab === 'open' ? (
                  <tr>
                    <th className="px-4 py-3 w-6"></th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized P&L</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-indigo-600 uppercase">Opened ↓</th>
                  </tr>
                ) : (
                  <tr>
                    <th className="px-4 py-3 w-6"></th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Net Size</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Result</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized P&L</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-indigo-600 uppercase">Opened ↓</th>
                  </tr>
                )}
              </thead>
              <tbody className="divide-y divide-gray-200">
                {positions.map(p => {
                  const pnl = parseFloat(p.realized_pnl);
                  const trades = tradesByToken.get(p.token_id) || [];
                  const isExpanded = expanded.has(p.id);

                  return (
                    <Fragment key={p.id}>
                      <tr
                        className="hover:bg-gray-50 cursor-pointer select-none"
                        onClick={() => toggleExpand(p.id)}
                      >
                        <td className="px-4 py-3 text-gray-400 text-xs">
                          {trades.length > 0 ? (isExpanded ? '▾' : '▸') : ''}
                        </td>
                        <td className="px-4 py-3 max-w-[260px]">
                          {p.question
                            ? <span className="text-sm" title={p.question}>
                                {p.question.length > 60 ? p.question.slice(0, 60) + '…' : p.question}
                              </span>
                            : <span className="text-xs font-mono text-gray-400" title={p.token_id}>
                                {p.token_id.slice(0, 14)}…
                              </span>
                          }
                        </td>
                        <td className="px-4 py-3 font-medium">{p.outcome}</td>

                        {activeTab === 'open' ? (
                          <>
                            <td className="px-4 py-3">{parseFloat(p.size) > 0 ? parseFloat(p.size).toFixed(2) : '—'}</td>
                            <td className="px-4 py-3">{p.avg_price}</td>
                            <td className={`px-4 py-3 font-medium ${pnl > 0 ? 'text-green-600' : pnl < 0 ? 'text-red-600' : 'text-gray-400'}`}>
                              {pnl !== 0 ? `${pnl > 0 ? '+' : ''}$${pnl.toFixed(2)}` : '—'}
                            </td>
                          </>
                        ) : (
                          <>
                            <td className="px-4 py-3">{parseFloat(p.net_size).toFixed(2)}</td>
                            <td className="px-4 py-3">{p.avg_price}</td>
                            <td className="px-4 py-3"><ResultBadge winner={p.winner} /></td>
                            <td className={`px-4 py-3 font-medium ${pnl > 0 ? 'text-green-600' : pnl < 0 ? 'text-red-600' : 'text-gray-400'}`}>
                              {pnl !== 0 ? `${pnl > 0 ? '+' : ''}$${pnl.toFixed(2)}` : '—'}
                            </td>
                          </>
                        )}

                        <td className="px-4 py-3 text-gray-500 text-sm">
                          {p.created_at ? formatDate(p.created_at) : '—'}
                        </td>
                      </tr>

                      {isExpanded && trades.length > 0 && (
                        <tr>
                          <td colSpan={activeTab === 'open' ? 7 : 8} className="px-0 py-0 bg-gray-50">
                            <div className="px-8 py-3">
                              <table className="min-w-full text-xs">
                                <thead>
                                  <tr className="text-gray-400 uppercase">
                                    <th className="px-2 py-1 text-left">Time</th>
                                    <th className="px-2 py-1 text-left">Side</th>
                                    <th className="px-2 py-1 text-left">Price</th>
                                    <th className="px-2 py-1 text-left">Size</th>
                                    <th className="px-2 py-1 text-left">Result</th>
                                    <th className="px-2 py-1 text-left">P&L</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y divide-gray-100">
                                  {trades.map(t => (
                                    <tr key={t.id} className="text-gray-700">
                                      <td className="px-2 py-1.5 text-gray-500">
                                        {new Date(t.match_time).toLocaleString()}
                                      </td>
                                      <td className={`px-2 py-1.5 font-medium ${t.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>
                                        {t.side}
                                      </td>
                                      <td className="px-2 py-1.5">{t.price}</td>
                                      <td className="px-2 py-1.5">{t.size}</td>
                                      <td className="px-2 py-1.5">
                                        <TradeResultBadge side={t.side} winner={t.winner} />
                                      </td>
                                      <td className="px-2 py-1.5">
                                        {t.profit_loss !== null && t.profit_loss !== undefined
                                          ? <span className={t.profit_loss >= 0 ? 'text-green-600' : 'text-red-600'}>
                                              {t.profit_loss >= 0 ? '+' : ''}${t.profit_loss.toFixed(2)}
                                            </span>
                                          : <span className="text-gray-400">—</span>
                                        }
                                      </td>
                                    </tr>
                                  ))}
                                </tbody>
                              </table>
                            </div>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-gray-200 text-sm text-gray-600">
            <span>{total} positions · page {page} of {totalPages}</span>
            <div className="flex space-x-2">
              <button
                onClick={() => setPage(p => p - 1)}
                disabled={page === 1}
                className="px-3 py-1 rounded border disabled:opacity-40 hover:bg-gray-50"
              >
                Prev
              </button>
              <button
                onClick={() => setPage(p => p + 1)}
                disabled={page >= totalPages}
                className="px-3 py-1 rounded border disabled:opacity-40 hover:bg-gray-50"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
