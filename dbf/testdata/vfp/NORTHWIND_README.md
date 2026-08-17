ORDERS.DBF and SUPPLIERS.DBF, from ha1tch/VPFX-Samples/Northwind/.

These settled the _NullFlags bit-ordering question (T-34): bit N
corresponds to the Nth nullable field in declaration order,
confirmed independently on five different partially-null fields
(orders: SHIPPEDDAT, SHIPREGION, SHIPPOSTAL; suppliers: REGION,
FAX) with zero exceptions across every record.
