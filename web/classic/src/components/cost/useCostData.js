/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import dayjs from 'dayjs';
import { API, showError } from '../../helpers';

const DEFAULT_PAGE_SIZE = 20;

// 默认时间范围：近 7 天
const getDefaultDateRange = () => [
  dayjs().subtract(6, 'day').startOf('day').toDate(),
  dayjs().endOf('day').toDate(),
];

export const useCostData = () => {
  const { t } = useTranslation();

  // ========== 查询条件 ==========
  const [dateRange, setDateRangeState] = useState(getDefaultDateRange);
  const [activeTab, setActiveTabState] = useState('users'); // users | models | channels
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);

  // ========== 数据 ==========
  const [overview, setOverview] = useState(null);
  const [overviewLoading, setOverviewLoading] = useState(false);
  const [pageData, setPageData] = useState({
    items: [],
    total: 0,
    page: 1,
    page_size: DEFAULT_PAGE_SIZE,
    summary: null,
  });
  const [tableLoading, setTableLoading] = useState(false);

  const startTimestamp = Math.floor((dateRange[0]?.getTime() || 0) / 1000);
  const endTimestamp = Math.floor((dateRange[1]?.getTime() || 0) / 1000);

  // ========== 加载函数 ==========
  const loadOverview = useCallback(async () => {
    setOverviewLoading(true);
    try {
      const res = await API.get('/api/cost/overview', {
        params: {
          start_timestamp: startTimestamp,
          end_timestamp: endTimestamp,
        },
      });
      const { success, message, data } = res.data;
      if (success) {
        setOverview(data);
      } else {
        showError(message);
      }
    } finally {
      setOverviewLoading(false);
    }
  }, [startTimestamp, endTimestamp]);

  const loadDimension = useCallback(async () => {
    setTableLoading(true);
    try {
      const res = await API.get(`/api/cost/${activeTab}`, {
        params: {
          start_timestamp: startTimestamp,
          end_timestamp: endTimestamp,
          p: activePage,
          page_size: pageSize,
        },
      });
      const { success, message, data } = res.data;
      if (success) {
        setPageData(data);
      } else {
        showError(message);
      }
    } finally {
      setTableLoading(false);
    }
  }, [activeTab, activePage, pageSize, startTimestamp, endTimestamp]);

  useEffect(() => {
    loadOverview();
  }, [loadOverview]);

  useEffect(() => {
    loadDimension();
  }, [loadDimension]);

  // ========== 交互函数 ==========
  const setActiveTab = (tab) => {
    setActiveTabState(tab);
    setActivePage(1);
  };

  const handlePageChange = (page) => setActivePage(page);
  const handlePageSizeChange = (size) => {
    setPageSize(size);
    setActivePage(1);
  };

  // 由查询表单提交时调用，提交后统一刷新
  const applyDateRange = (range) => {
    if (Array.isArray(range) && range.length === 2 && range[0] && range[1]) {
      setDateRangeState(range);
      setActivePage(1);
    }
  };

  const refresh = () => {
    loadOverview();
    loadDimension();
  };

  return {
    t,
    dateRange,
    applyDateRange,
    activeTab,
    setActiveTab,
    activePage,
    pageSize,
    handlePageChange,
    handlePageSizeChange,
    overview,
    overviewLoading,
    pageData,
    tableLoading,
    refresh,
    loadOverview,
    loadDimension,
  };
};

export default useCostData;
