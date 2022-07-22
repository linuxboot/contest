import React from 'react';
import { Cell, CellProps } from 'rsuite-table';

export default function DateCell({ rowData, dataKey, ...props }: CellProps) {
    return (
        <Cell {...props}>
            <p>{dataKey && new Date(rowData[dataKey]).toLocaleString()}</p>
        </Cell>
    );
}
