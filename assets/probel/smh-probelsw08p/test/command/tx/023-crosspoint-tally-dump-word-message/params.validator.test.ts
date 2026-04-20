import { BootstrapService } from '../../../../src/bootstrap.service';
import { CrossPointTallyDumpWordCommandParams } from '../../../../src/command/tx/023-crosspoint-tally-dump-word-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/tx/023-crosspoint-tally-dump-word-message/params.validator';
import { CrossPointTallyDumpWordCommand } from '../../../../src/command/tx/023-crosspoint-tally-dump-word-message/command';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { BufferUtility } from '../../../../src/common/utility/buffer.utility';

describe('CrossPoint Tally Dump (Word) Message - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItem
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 63,
                numberOfTalliesReturned: 64,
                sourceIdItems: buildDataArray
            };

            // Act

            const validator = new CommandParamsValidator(params);
            const metaCommand = new CrossPointTallyDumpWordCommand(params);
            metaCommand.buildCommand();

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params - ...', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 63,
                numberOfTalliesReturned: 64,
                sourceIdItems: buildDataArray
            };

            // Act

            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();
            const metaCommand = new CrossPointTallyDumpWordCommand(params);
            metaCommand.buildCommand();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(-1);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstDestinationId: -1,
                numberOfTalliesReturned: 0,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.numberOfTalliesReturned).toBeDefined();
            expect(errors.numberOfTalliesReturned.id).toBe(
                CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.sourceIdAndMaximumNumberOfSources).toBeDefined();
            expect(errors.sourceIdAndMaximumNumberOfSources.id).toBe(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceIdItems).toBeDefined();
            expect(errors.sourceIdItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add sourceItems
                    buildDataArray.push(65536);
                }
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstDestinationId: 65536,
                numberOfTalliesReturned: 65,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.numberOfTalliesReturned).toBeDefined();
            expect(errors.numberOfTalliesReturned.id).toBe(
                CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.sourceIdAndMaximumNumberOfSources).toBeDefined();
            expect(errors.sourceIdAndMaximumNumberOfSources.id).toBe(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceIdItems).toBeDefined();
            expect(errors.sourceIdItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors SourceId + Number of Tallies retuned params are out of range', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 32; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(65530);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 65530,
                numberOfTalliesReturned: 64,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.sourceIdAndMaximumNumberOfSources).toBeDefined();
            expect(errors.sourceIdAndMaximumNumberOfSources.id).toBe(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
        });

        it('Should return errors sourceIdItems params is empty', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 0,
                numberOfTalliesReturned: 64,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.sourceIdItems).toBeDefined();
            expect(errors.sourceIdItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
