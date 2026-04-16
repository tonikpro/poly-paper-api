import { useEffect, useState, useCallback } from 'react';
import { getOrders, cancelOrder } from '../api/client';

interface Order {
  id: string;
  side: string;
  outcome: string;
  price: string;
  original_size: string;
  size_matched: string;
  order_type: string;
  created_at: string;
  asset_id: string;
  status: string;
  question: string;
}

const TABS = ['All', 'Live', 'Matched', 'Canceled'] as const;
type Tab = typeof TABS[number];

const STATUS_PARAM: Record<Tab, string | undefined> = {
  All: undefined,
  Live: 'LIVE',
  Matched: 'MATCHED',
  Canceled: 'CANCELED',
};

const PAGE_SIZE = 20;

function StatusBadge({ status }: { status: string }) {
  const cls =
    status === 'LIVE'     ? 'bg-blue-100 text-blue-700' :
    status === 'MATCHED'  ? 'bg-green-100 text-green-700' :
    status === 'CANCELED' ? 'bg-gray-100 text-gray-500' :
                            'bg-gray-100 text-gray-500';
  return (
    <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${cls}`}>
      {status}
    </span>
  );
}

export default function Orders() {
  const [tab, setTab] = useState<Tab>('Live');
  const [orders, setOrders] = useState<Order[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);

  const fetchOrders = useCallback(() => {
    const status = STATUS_PARAM[tab];
    getOrders({ status, limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE })
      .then(r => {
        setOrders(r.data.orders || []);
        setTotal(r.data.total || 0);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [tab, page]);

  useEffect(() => {
    setLoading(true);
    fetchOrders();
  }, [fetchOrders]);

  // Auto-refresh only on Live tab
  useEffect(() => {
    if (tab !== 'Live') return;
    const interval = setInterval(fetchOrders, 5000);
    return () => clearInterval(interval);
  }, [tab, fetchOrders]);

  // Reset to page 1 when tab changes
  useEffect(() => { setPage(1); }, [tab]);

  const handleCancel = (id: string) => {
    setOrders(prev => prev.filter(o => o.id !== id));
    cancelOrder(id).catch(() => fetchOrders());
  };

  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Orders</h1>

      {/* Tabs */}
      <div className="flex space-x-1 mb-4 border-b border-gray-200">
        {TABS.map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
              tab === t
                ? 'border-blue-500 text-blue-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {loading ? (
        <div className="text-center py-8">Loading...</div>
      ) : orders.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No orders.
        </div>
      ) : (
        <>
          <div className="bg-white rounded-lg shadow overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Side</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Filled</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {orders.map(o => (
                  <tr key={o.id} className="hover:bg-gray-50">
                    <td className="px-4 py-3 max-w-[220px]">
                      {o.question
                        ? <span className="text-sm" title={o.question}>
                            {o.question.length > 50 ? o.question.slice(0, 50) + '…' : o.question}
                          </span>
                        : <span className="text-xs font-mono text-gray-400" title={o.asset_id}>
                            {o.asset_id?.slice(0, 12)}…
                          </span>
                      }
                    </td>
                    <td className={`px-4 py-3 font-medium ${o.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>
                      {o.side}
                    </td>
                    <td className="px-4 py-3 text-sm">{o.outcome || '—'}</td>
                    <td className="px-4 py-3">{o.price}</td>
                    <td className="px-4 py-3">{parseFloat(o.original_size).toFixed(2)}</td>
                    <td className="px-4 py-3 text-sm">
                      {parseFloat(o.size_matched).toFixed(2)} / {parseFloat(o.original_size).toFixed(2)}
                    </td>
                    <td className="px-4 py-3"><StatusBadge status={o.status} /></td>
                    <td className="px-4 py-3 text-sm">{o.order_type}</td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      {new Date(o.created_at).toLocaleString()}
                    </td>
                    <td className="px-4 py-3">
                      {o.status === 'LIVE' && (
                        <button
                          onClick={() => handleCancel(o.id)}
                          className="text-xs text-red-600 hover:text-red-800 border border-red-200 hover:border-red-400 px-2 py-1 rounded"
                        >
                          Cancel
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 text-sm text-gray-600">
              <span>{total} orders · page {page} of {totalPages}</span>
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
