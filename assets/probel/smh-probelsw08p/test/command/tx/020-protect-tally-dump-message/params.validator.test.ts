import { BootstrapService } from '../../../../src/bootstrap.service';
import { ProtectTallyDumpCommandParams } from '../../../../src/command/tx/020-protect-tally-dump-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/tx/020-protect-tally-dump-message/params.validator';
import { ProtectTallyDumpCommand } from '../../../../src/command/tx/020-protect-tally-dump-message/command';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { ProtectTallyDumpCommandItems } from '../../../../src/command/tx/020-protect-tally-dump-message/items';
import { BufferUtility } from '../../../../src/common/utility/buffer.utility';

describe('Protect Tally Dump Message - CommandOptionsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: itemIndex * 4, protectedData: itemIndex });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 63,
                numberOfProtectTallies: 64,
                deviceNumberProtectDataItems: buildDataArray
            };

            // Act

            const validator = new CommandParamsValidator(params);
            const metaCommand = new ProtectTallyDumpCommand(params);
            metaCommand.buildCommand();

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params - ...', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: itemIndex * 4, protectedData: itemIndex });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 63,
                numberOfProtectTallies: 64,
                deviceNumberProtectDataItems: buildDataArray
            };

            // Act

            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();
            const metaCommand = new ProtectTallyDumpCommand(params);
            metaCommand.buildCommand();


            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: -1, protectedData: -1 });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstDestinationId: -1,
                numberOfProtectTallies: 0,
                deviceNumberProtectDataItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.numberOfProtectTallies).toBeDefined();
            expect(errors.numberOfProtectTallies.id).toBe(
                CommandErrorsKeys.NUMBER_OF_PROTECT_TALLIES_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.deviceNumberProtectDataItems).toBeDefined();
            expect(errors.deviceNumberProtectDataItems.id).toBe(
                CommandErrorsKeys.DEVICE_NUMBER_AND_PROTECT_DETAILS_ARE_OUT_OF_RANGE_ERROR_MSG
            );
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: 1024, protectedData: 5 });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstDestinationId: 65536,
                numberOfProtectTallies: 65,
                deviceNumberProtectDataItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.numberOfProtectTallies).toBeDefined();
            expect(errors.numberOfProtectTallies.id).toBe(
                CommandErrorsKeys.NUMBER_OF_PROTECT_TALLIES_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.deviceNumberProtectDataItems).toBeDefined();
            expect(errors.deviceNumberProtectDataItems.id).toBe(
                CommandErrorsKeys.DEVICE_NUMBER_AND_PROTECT_DETAILS_ARE_OUT_OF_RANGE_ERROR_MSG
            );
        });

        it('Should return errors protectedData params are out of range', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: 1, protectedData: -1 });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 0,
                numberOfProtectTallies: 64,
                deviceNumberProtectDataItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.deviceNumberProtectDataItems).toBeDefined();
            expect(errors.deviceNumberProtectDataItems.id).toBe(
                CommandErrorsKeys.DEVICE_NUMBER_AND_PROTECT_DETAILS_ARE_OUT_OF_RANGE_ERROR_MSG
            );
        });

        it('Should return errors isDeviceNumberProtectDataItems params is empty', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 0,
                numberOfProtectTallies: 64,
                deviceNumberProtectDataItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.deviceNumberProtectDataItems).toBeDefined();
            expect(errors.deviceNumberProtectDataItems.id).toBe(
                CommandErrorsKeys.DEVICE_NUMBER_AND_PROTECT_DETAILS_ARE_OUT_OF_RANGE_ERROR_MSG
            );
        });
    });
});
