import React from 'react';
import { Cell, CellProps } from 'rsuite-table';

function resolveDatakey(rowData: any, dataKey?: string): string | undefined {
    if (!rowData || !dataKey) return undefined;
    if (dataKey.split('.').length == 1) return rowData[dataKey];
    let keys = dataKey.split('.');
    return resolveDatakey(rowData[keys[0]], keys.slice(1).join('.'));
}

export default function DateCell({ rowData, dataKey, ...props }: CellProps) {
    let data = dataKey && resolveDatakey(rowData, dataKey);
    return (
        <Cell {...props}>
            <p>{data && new Date(data).toLocaleString()}</p>
        </Cell>
    );
}
