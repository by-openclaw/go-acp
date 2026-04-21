import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointTallyDumpWordCommandParams } from './params';

/**
 * Crosspoint Tally Dump (Word) Message Validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointTallyDumpWordCommandParams>}
 */
export class CommandParamsValidator implements IValidator<CrossPointTallyDumpWordCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointTallyDumpWordCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointTallyDumpWordCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isTalliesReturnedOutOfRange()) {
            errors.numberOfTalliesReturned = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isDestinationIdOutOfRange()) {
            errors.destinationId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceIdAndnumberOfTalliesReturnedOutOfRange()) {
            errors.sourceIdAndMaximumNumberOfSources = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        // TODO : if out of range need to create an array of sourceNamesItems
        if (this.isSourceItemsOutOfRange()) {
            errors.sourceIdItems = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * returns a boolean indicating if matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if matrixId is out of range
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     * Gets a boolean indicating whether the levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }

    /**
     * Gets a boolean indicating whether the numberOfTalliesReturned is out of range
     * + false = numberOfTalliesReturned is included within the limits
     * + true = numberOfTalliesReturned  [1-64] is out of range
     * @private
     * @returns {boolean} indicating if numberOfTalliesReturned is out of range
     * @memberof export class CrossPointConnectedMessageCommandParamsValidator

     */
    private isTalliesReturnedOutOfRange(): boolean {
        return this.data.numberOfTalliesReturned < 1 || this.data.numberOfTalliesReturned > 64; // could be 65535
    }

    /**
     * Gets a boolean indicating whether the firstDestinationId is out of range
     * + false = firstDestinationId is included within the limits
     * + true = firstDestinationId  [0-65535] is out of range
     * @private
     * @returns {boolean} indicating if firstDestinationId is out of range
     * @memberof export class CrossPointConnectedMessageCommandParamsValidator

     */
    private isDestinationIdOutOfRange(): boolean {
        return this.data.firstDestinationId < 0 || this.data.firstDestinationId > 65535;
    }

    /**
     * Gets a boolean indicating whether the sourceId + number of source Names to follow is out of range
     * + false = sourceId + numberOfTalliesReturned are included within the limits
     * + true = sourceId  + numberOfTalliesReturned [0-65535] is out of range
     * @private
     * @returns {boolean} indicating if sourceId + number of source Names to follow is out of range
     * @memberof CommandParamsValidator
     */
    private isSourceIdAndnumberOfTalliesReturnedOutOfRange(): boolean {
        return (
            this.data.firstDestinationId + this.data.numberOfTalliesReturned < 1 ||
            this.data.firstDestinationId + this.data.numberOfTalliesReturned > 65535
        );
    }

    /**
     * Gets a boolean indicating whether the sourceIdItems is out of range
     * + false = sourceIdItems is included within the limits
     * + true = sourceIdItems is out of range
     * @private
     * @returns {boolean} indicating if sourceIdItems length is out of range
     * @memberof CommandParamsValidator
     */
    private isSourceItemsOutOfRange(): boolean {
        if (this.data.sourceIdItems.length === 0) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.sourceIdItems.length; byteIndex++) {
            const data = this.data.sourceIdItems[byteIndex];
            if (data < 0 || data > 65535) {
                return true;
            }
        }
        return false;
    }
}
