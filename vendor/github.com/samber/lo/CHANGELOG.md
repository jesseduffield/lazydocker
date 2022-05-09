# Changelog

@samber: I sometimes forget to update this file. Ping me on [Twitter](https://twitter.com/samuelberthe) or open an issue in case of error. We need to keep a clear changelog for easier lib upgrade.

## 1.20.0 (2022-05-02)

Adding:

- lo.Synchronize
- lo.SumBy

Change:
- Removed generic type definition for lo.Try0: `lo.Try0[T]()` -> `lo.Try0()`

## 1.19.0 (2022-04-30)

Adding:

- lo.RepeatBy
- lo.Subset
- lo.Replace
- lo.ReplaceAll
- lo.Substring
- lo.RuneLength

## 1.18.0 (2022-04-28)

Adding:

- lo.SomeBy
- lo.EveryBy
- lo.None
- lo.NoneBy

## 1.17.0 (2022-04-27)

Adding:

- lo.Unpack2 -> lo.Unpack3
- lo.Async0 -> lo.Async6

## 1.16.0 (2022-04-26)

Adding:

- lo.AttemptWithDelay

## 1.15.0 (2022-04-22)

Improvement:

- lo.Must: error or boolean value

## 1.14.0 (2022-04-21)

Adding:

- lo.Colaesce

## 1.13.0 (2022-04-14)

Adding:

- PickBy
- PickByKeys
- PickByValues
- OmitBy
- OmitByKeys
- OmitByValues
- Clamp
- MapKeys
- Invert
- IfF + ElseIfF + ElseF
- T0() + T1() + T2() + T3() + ...

## 1.12.0 (2022-04-12)

Adding:

- Must
- Must{0-6}
- FindOrElse
- Async
- MinBy
- MaxBy
- Count
- CountBy
- FindIndexOf
- FindLastIndexOf
- FilterMap

## 1.11.0 (2022-03-11)

Adding:

- Try
- Try{0-6}
- TryWitchValue
- TryCatch
- TryCatchWitchValue
- Debounce
- Reject

## 1.10.0 (2022-03-11)

Adding:

- Range
- RangeFrom
- RangeWithSteps

## 1.9.0 (2022-03-10)

Added

- Drop
- DropRight
- DropWhile
- DropRightWhile

## 1.8.0 (2022-03-10)

Adding Union.

## 1.7.0 (2022-03-09)

Adding ContainBy

Adding MapValues

Adding FlatMap

## 1.6.0 (2022-03-07)

Fixed PartitionBy.

Adding Sample

Adding Samples

## 1.5.0 (2022-03-07)

Adding Times

Adding Attempt

Adding Repeat

## 1.4.0 (2022-03-07)

- adding tuple types (2->9)
- adding Zip + Unzip
- adding lo.PartitionBy + lop.PartitionBy
- adding lop.GroupBy
- fixing Nth

## 1.3.0 (2022-03-03)

Last and Nth return errors

## 1.2.0 (2022-03-03)

Adding `lop.Map` and `lop.ForEach`.

## 1.1.0 (2022-03-03)

Adding `i int` param to `lo.Map()`, `lo.Filter()`, `lo.ForEach()` and `lo.Reduce()` predicates.

## 1.0.0 (2022-03-02)

*Initial release*

Supported helpers for slices:

- Filter
- Map
- Reduce
- ForEach
- Uniq
- UniqBy
- GroupBy
- Chunk
- Flatten
- Shuffle
- Reverse
- Fill
- ToMap

Supported helpers for maps:

- Keys
- Values
- Entries
- FromEntries
- Assign (maps merge)

Supported intersection helpers:

- Contains
- Every
- Some
- Intersect
- Difference

Supported search helpers:

- IndexOf
- LastIndexOf
- Find
- Min
- Max
- Last
- Nth

Other functional programming helpers:

- Ternary (1 line if/else statement)
- If / ElseIf / Else
- Switch / Case / Default
- ToPtr
- ToSlicePtr

Constraints:

- Clonable
