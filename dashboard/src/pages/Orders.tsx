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
}

export default function Orders() {
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchOrders = useCallback(() => {
    getOrders({ status: 'LIVE' })
      .then(r => setOrders(r.data.orders || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    fetchOrders();
    const interval = setInterval(fetchOrders, 5000);
    return () => clearInterval(interval);
  }, [fetchOrders]);

  const handleCancel = (id: string) => {
    setOrders(prev => prev.filter(o => o.id !== id));
    cancelOrder(id).catch(() => fetchOrders()); // re-fetch if cancel fails
  };

  if (loading) return <div className="text-center py-8">Loading...</div>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Live Orders</h1>
      {orders.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No live orders.
        </div>
      ) : (
        <div className="bg-white rounded-lg shadow overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Side</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Filled</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Action</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {orders.map(o => (
                <tr key={o.id} className="hover:bg-gray-50">
                  <td className={`px-4 py-3 font-medium ${o.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>
                    {o.side}
                  </td>
                  <td className="px-4 py-3 text-sm">{o.outcome || '—'}</td>
                  <td className="px-4 py-3">{o.price}</td>
                  <td className="px-4 py-3">{parseFloat(o.original_size).toFixed(2)}</td>
                  <td className="px-4 py-3 text-sm">
                    {parseFloat(o.size_matched).toFixed(2)} / {parseFloat(o.original_size).toFixed(2)}
                  </td>
                  <td className="px-4 py-3 text-sm">{o.order_type}</td>
                  <td className="px-4 py-3 text-xs text-gray-500">
                    {new Date(o.created_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => handleCancel(o.id)}
                      className="text-xs text-red-600 hover:text-red-800 border border-red-200 hover:border-red-400 px-2 py-1 rounded"
                    >
                      Cancel
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
