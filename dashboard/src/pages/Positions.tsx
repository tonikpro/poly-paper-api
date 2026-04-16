import { useEffect, useState } from 'react';
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

const PAGE_SIZE = 20;

function StatusBadge({ isOpen, winner }: { isOpen: boolean; winner: boolean | null }) {
  if (isOpen) return <span className="bg-blue-100 text-blue-700 text-xs px-2 py-0.5 rounded-full font-medium">Open</span>;
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

export default function Positions() {
  const [positions, setPositions] = useState<Position[]>([]);
  const [tradesByToken, setTradesByToken] = useState<Map<string, Trade[]>>(new Map());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([
      getPositions(),
      getTrades({ limit: 1000 }),
    ])
      .then(([posRes, tradeRes]) => {
        setPositions(posRes.data.positions || []);

        const map = new Map<string, Trade[]>();
        for (const t of (tradeRes.data.trades || []) as Trade[]) {
          const list = map.get(t.asset_id) || [];
          list.push(t);
          map.set(t.asset_id, list);
        }
        setTradesByToken(map);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const toggleExpand = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  if (loading) return <div className="text-center py-8">Loading...</div>;

  const totalPages = Math.ceil(positions.length / PAGE_SIZE);
  const paginated = positions.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Positions</h1>

      {positions.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No positions yet.
        </div>
      ) : (
        <>
          <div className="bg-white rounded-lg shadow overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-3 w-6"></th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized P&L</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {paginated.map(p => {
                  const pnl = parseFloat(p.realized_pnl);
                  const trades = tradesByToken.get(p.token_id) || [];
                  const isExpanded = expanded.has(p.id);

                  return (
                    <>
                      <tr
                        key={p.id}
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
                        <td className="px-4 py-3">{parseFloat(p.size).toFixed(2)}</td>
                        <td className="px-4 py-3">{p.avg_price}</td>
                        <td className="px-4 py-3">
                          <StatusBadge isOpen={p.is_open} winner={p.winner} />
                        </td>
                        <td className={`px-4 py-3 font-medium ${pnl > 0 ? 'text-green-600' : pnl < 0 ? 'text-red-600' : 'text-gray-400'}`}>
                          {pnl !== 0 ? `${pnl > 0 ? '+' : ''}$${pnl.toFixed(2)}` : '—'}
                        </td>
                      </tr>

                      {isExpanded && trades.length > 0 && (
                        <tr key={`${p.id}-trades`}>
                          <td colSpan={7} className="px-0 py-0 bg-gray-50">
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
                    </>
                  );
                })}
              </tbody>
            </table>
          </div>

          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 text-sm text-gray-600">
              <span>{positions.length} positions · page {page} of {totalPages}</span>
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
        </>
      )}
    </div>
  );
}
